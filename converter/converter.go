package converter

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"go/types"
	"hash/fnv"
	"maps"
	"slices"
	"strings"
	"text/template"

	"github.com/refaktor/ryegen/v2/converter/walktypes"
)

var (
	ErrInternalPackage = errors.New("use of internal package")
	ErrUnexported      = errors.New("use of unexported name")
	ErrGeneric         = errors.New("use of generic declaration")
	ErrCGo             = errors.New("use of CGo")
	ErrInvalidType     = errors.New("use of invalid type")
)

type Direction uint8

const (
	ToRye Direction = iota
	FromRye
)

// String returns the PascalCase string representation ("ToRye" or "FromRye").
func (d Direction) String() string {
	switch d {
	case ToRye:
		return "ToRye"
	case FromRye:
		return "FromRye"
	default:
		panic("invalid conversion direction")
	}
}

// StringCamelCase returns the camelCase string representation ("toRye" or "fromRye").
func (d Direction) StringCamelCase() string {
	switch d {
	case ToRye:
		return "toRye"
	case FromRye:
		return "fromRye"
	default:
		panic("invalid conversion direction")
	}
}

func (d Direction) Opposite() Direction {
	switch d {
	case ToRye:
		return FromRye
	case FromRye:
		return ToRye
	default:
		panic("invalid conversion direction")
	}
}

func typeHash(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	return fmt.Sprintf("%016x", h.Sum64())
}

// Returns an error if:
//   - Any internal or unexported component is required
//     to express the type in its Go representation
//   - The type uses any generics
//   - CGo is required
func checkConvertible(t types.Type) error {
	checkPkg := func(pkg *types.Package) error {
		if pkg == nil {
			return nil
		}
		if pkg.Path() == "C" {
			return ErrCGo
		}
		for sp := range strings.SplitSeq(pkg.Path(), "/") {
			if sp == "internal" {
				return ErrInternalPackage
			}
		}
		return nil
	}
	checkVar := func(v *types.Var) error {
		if v.Pkg().Scope() != types.Universe && !v.Exported() {
			return ErrUnexported
		}
		return checkPkg(v.Pkg())
	}
	checkTypeName := func(tn *types.TypeName) error {
		if tn.Pkg().Scope() != types.Universe && !tn.Exported() {
			return ErrUnexported
		}
		return checkPkg(tn.Pkg())
	}

	var stack []types.Type
	var check func(t types.Type) error
	check = func(t types.Type) error {
		if slices.Contains(stack, t) {
			// Break recursion loops
			return nil
		}
		stack = append(stack, t)
		defer func() {
			stack = (stack)[:len(stack)-1]
		}()

		switch t := t.(type) {
		case *types.Basic:
			if t.Kind() == types.Invalid {
				return ErrInvalidType
			}
		case *types.Alias:
			if t.TypeParams() != nil {
				return ErrGeneric
			}
			if err := checkTypeName(t.Obj()); err != nil {
				return err
			}
		case *types.Struct:
			for v := range t.Fields() {
				if err := checkVar(v); err != nil {
					return err
				}
			}
		case *types.Signature:
			if t.TypeParams() != nil || t.RecvTypeParams() != nil {
				return ErrGeneric
			}
		case *types.Named:
			if t.TypeParams() != nil {
				return ErrGeneric
			}
			if err := checkTypeName(t.Obj()); err != nil {
				return err
			}
		}

		return walktypes.Walk(t, check)
	}

	return check(t)
}

// e.g. github.com/someone/somerepo => github_com_someone_somerepo
func packagePathToImportName(path string) string {
	return strings.NewReplacer(
		"/", "_",
		".", "_",
		"-", "_",
	).Replace(path)
}

// Applies some transformations on a type before
// it gets passed to the converter.
func normalizeType(typ types.Type) (types.Type, error) {
	// Aliased types always behave exactly the same as the
	// type R in "type A = R", even if they're nested within
	// other types.
	typ = types.Unalias(typ)

	switch t := typ.(type) {
	case *types.Signature:
		if t.Recv() != nil {
			// Turn receiver into regular parameter for conversion.
			params := append([]*types.Var{t.Recv()}, slices.Collect(t.Params().Variables())...)
			if t.RecvTypeParams() != nil || t.TypeParams() != nil {
				return nil, ErrGeneric
			}
			//typeParams := append(slices.Collect(t.RecvTypeParams().TypeParams()), slices.Collect(t.TypeParams().TypeParams())...)
			typ = types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), t.Results(), t.Variadic())
		}
	case *types.Interface:
		if t.NumMethods() == 0 {
			typ = types.Universe.Lookup("any").Type()
			return typ, nil
		}
	}

	return walktypes.WalkModify(typ, normalizeType)
}

type namedType struct {
	pkg  string
	name string
}

type convKey struct {
	typString string // result of types.Type.String()
	dir       Direction
}

// generated converter
type conv struct {
	key  convKey
	typ  types.Type
	code []byte
}

type convSpec struct {
	typ types.Type
	dir Direction
}

type ConverterSet struct {
	convs       map[convKey]conv
	importPaths map[string]struct{}
	namedTypes  map[namedType]struct{}
	tmplToRye   *template.Template
	tmplFromRye *template.Template
	basePkg     string

	// newImportPaths/newDeps are the destination slices
	// for template-based dependency tracking. This
	// is kind of a hack, which allows detecting
	// dependencies directly from execution of funcs
	// int the template's FuncMap. The slices have to
	// be reused constantly, so it's best not to touch
	// them directly.
	newImportPaths []string
	newDeps        []convSpec
}

// NewConverterSet creates a new [ConverterSet].
// basePkg is the package path Ryegen was initiated
// in (usually "main").
func NewConverterSet(basePkg string) *ConverterSet {
	cs := &ConverterSet{
		convs:       map[convKey]conv{},
		importPaths: map[string]struct{}{},
		namedTypes:  map[namedType]struct{}{},
		basePkg:     basePkg,
	}

	// Set up template functions with dependency tracking
	funcs := maps.Clone(templateFuncMap)
	funcs["conv"] = func(typ types.Type, dir Direction) string {
		cs.newDeps = append(cs.newDeps, convSpec{typ, dir})
		return cs.convName(typ, dir)
	}
	funcs["objStr"] = func(obj types.Object) string {
		return types.ObjectString(
			obj,
			cs.ImportNameQualifier,
		)
	}
	funcs["typStr"] = func(t types.Type) string {
		var collectImports func(t types.Type) error
		collectImports = func(t types.Type) error {
			if t, ok := t.(*types.Named); ok {
				if pkg := t.Obj().Pkg(); pkg != nil {
					path := pkg.Path()
					if path != cs.basePkg {
						cs.newImportPaths = append(cs.newImportPaths, path)
					}
				}
			}
			return walktypes.Walk(t, collectImports)
		}
		if err := collectImports(t); err != nil {
			panic("programmer error: expected no error, but got: " + err.Error())
		}
		return types.TypeString(
			t,
			cs.ImportNameQualifier,
		)
	}

	cs.tmplToRye = template.Must(template.New("to_rye.tmpl").Funcs(funcs).Parse(templateSrcToRye))
	cs.tmplFromRye = template.Must(template.New("from_rye.tmpl").Funcs(funcs).Parse(templateSrcFromRye))

	return cs
}

func (cs *ConverterSet) typeUniqueName(typ types.Type) string {
	switch typ := typ.(type) {
	case *types.Alias:
		if typ.Obj().Pkg() == nil && typ.Obj().Name() == "any" {
			return "any"
		}
	case *types.Basic:
		if typ.Kind() == types.Invalid {
			return "invalid"
		}
		return typ.Name()
	case *types.Pointer:
		return fmt.Sprintf("ptr_%v", cs.typeUniqueName(typ.Elem()))
	case *types.Named:
		if typ.Obj().Pkg() != nil {
			return fmt.Sprintf("%v_%v", cs.ImportNameQualifier(typ.Obj().Pkg()), typ.Obj().Name())
		} else {
			return typ.Obj().Name()
		}
	case *types.Signature:
		return fmt.Sprintf("func_%v", typeHash(typ.String()))
	case *types.Map:
		return fmt.Sprintf("map_%v_%v", cs.typeUniqueName(typ.Key()), cs.typeUniqueName(typ.Elem()))
	case *types.Array:
		return fmt.Sprintf("array_%v_%v", typ.Len(), cs.typeUniqueName(typ.Elem()))
	case *types.Slice:
		return fmt.Sprintf("slice_%v", cs.typeUniqueName(typ.Elem()))
	case *types.Struct:
		return fmt.Sprintf("struct_%v", typeHash(typ.String()))
	case *types.Interface:
		return fmt.Sprintf("interface_%v", typeHash(typ.String()))
	}
	return fmt.Sprintf("unk_%v", typeHash(typ.String()))
}

func (cs *ConverterSet) convName(typ types.Type, dir Direction) string {
	return fmt.Sprintf("conv_%v_%v", cs.typeUniqueName(typ), dir.StringCamelCase())
}

// Returns the name of the template used for the given type.
func (cs *ConverterSet) templateName(typ types.Type) (string, error) {
	switch typ := typ.(type) {
	case *types.Alias:
		if typ.Obj().Pkg() == nil && typ.Obj().Name() == "any" {
			return "any", nil
		}
	case *types.Basic:
		switch {
		case typ.Info()&types.IsInteger != 0:
			return "integer", nil
		case typ.Info()&types.IsFloat != 0:
			return "float", nil
		case typ.Info()&types.IsBoolean != 0:
			return "bool", nil
		case typ.Info()&types.IsString != 0:
			return "string", nil
		}
	case *types.Pointer:
		return "pointer", nil
	case *types.Named:
		var pkgPath string
		if typ.Obj().Pkg() != nil {
			pkgPath = typ.Obj().Pkg().Path()
		}
		switch pkgPath {
		case "":
			switch typ.Obj().Name() {
			case "error":
				return "error", nil
			}
		case "time":
			if typ.Obj().Name() == "Time" {
				return "time", nil
			}
		}
		return "named", nil
	case *types.Map:
		if typ, ok := typ.Key().(*types.Basic); ok && typ.Kind() == types.String {
			return "map", nil
		} else {
			return "named", nil
		}
	case *types.Signature:
		if typ.Recv() != nil {
			panic("logic error: recv sholuld have been placed into params")
		}
		return "func", nil
	case *types.Array:
		return "array", nil
	case *types.Slice:
		return "slice", nil
	case *types.Struct:
		return "struct", nil
	}
	return "", fmt.Errorf("no known converter template for type %v", typ)
}

// executeTemplate executes the converter template tmpl on data and
// returns the generated code, and the collected converter dependencies
// and import dependencies.
func (cs *ConverterSet) executeTemplate(tmpl *template.Template, data types.Type) (code []byte, deps []convSpec, importPaths []string, err error) {
	defer func() {
		cs.newDeps = cs.newDeps[:0]
		cs.newImportPaths = cs.newImportPaths[:0]
	}()

	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, nil, nil, err
	}

	// New first-order converter dependencies and
	// imports have been collected in newDeps/newImports
	// by the template execution.
	deps = slices.Clone(cs.newDeps)
	importPaths = slices.Clone(cs.newImportPaths)

	return b.Bytes(), deps, importPaths, nil
}

// Registers the given type and its children in cs.namedTypes.
func (cs *ConverterSet) registerNamedType(typ types.Type) {
	var doRegisterNamedType func(typ types.Type) error
	doRegisterNamedType = func(typ types.Type) error {
		if typ, ok := typ.(*types.Named); ok {
			pkgPath := ""
			if pkg := typ.Obj().Pkg(); pkg != nil {
				pkgPath = pkg.Path()
			}
			cs.namedTypes[namedType{
				pkgPath,
				typ.Obj().Name()}] = struct{}{}
		}
		return walktypes.Walk(typ, doRegisterNamedType)
	}
	if err := doRegisterNamedType(typ); err != nil {
		panic("programmer error: expected no error, but got: " + err.Error())
	}
}

// Assumes typ to already be normalized and checked for convertibility.
func (cs *ConverterSet) genConvsNormalizedAndChecked(typ types.Type, dir Direction) (_convs []conv, _importPaths []string, _ error) {
	key := convKey{typ.String(), dir}
	if _, alreadyAdded := cs.convs[key]; alreadyAdded {
		return nil, nil, nil
	}

	var tmpl *template.Template
	switch dir {
	case ToRye:
		tmpl = cs.tmplToRye
	case FromRye:
		tmpl = cs.tmplFromRye
	default:
		panic("invalid conversion direction")
	}
	tmplName, err := cs.templateName(typ)
	if err != nil {
		return nil, nil, err
	}
	tmpl = tmpl.Lookup(tmplName)
	if tmpl == nil {
		return nil, nil, fmt.Errorf("no template to convert %v %v", tmplName, dir)
	}
	code, deps, importPaths, err := cs.executeTemplate(tmpl, typ)
	if err != nil {
		return nil, nil, fmt.Errorf("execute converter template for %v %v: %w", tmplName, dir, err)
	}

	resConvs := []conv{{key, typ, code}}
	resImportPaths := importPaths

	for _, dep := range deps {
		if dep.typ != typ || dep.dir != dir {
			// TODO: Consider switching to a graph-building approach,
			// for improved performance, but only if benchmarks say
			// it's reasonable to do so.
			convs, importPaths, err := cs.genConvsNormalizedAndChecked(dep.typ, dep.dir)
			if err != nil {
				return nil, nil, err
			}
			resConvs = append(resConvs, convs...)
			resImportPaths = append(resImportPaths, importPaths...)
		}
	}
	return resConvs, resImportPaths, nil
}

// ImportNameQualifier transforms any package name to
// the import path from the perspective of the converter
// set's base package. Returns an empty string if pkg is
// the universe or the base package.
func (cs *ConverterSet) ImportNameQualifier(pkg *types.Package) string {
	if pkg.Path() == cs.basePkg {
		return ""
	}
	return packagePathToImportName(pkg.Path())
}

// Add adds a converter to the ConverterSet, meaning it will end up
// in the generated code.
// converterName is the name of the resulting converter function.
func (cs *ConverterSet) Add(typ types.Type, dir Direction) (converterName string, _ error) {
	typ, err := normalizeType(typ)
	if err != nil {
		return "", err
	}
	if err := checkConvertible(typ); err != nil {
		return "", err
	}
	cs.registerNamedType(typ)
	convs, importPaths, err := cs.genConvsNormalizedAndChecked(typ, dir)
	if err != nil {
		return "", err
	}
	for _, conv := range convs {
		cs.convs[conv.key] = conv
	}
	for _, imp := range importPaths {
		cs.importPaths[imp] = struct{}{}
	}
	return cs.convName(typ, dir), nil
}

// Code returns all of the generated converter code.
func (cs *ConverterSet) Code() []byte {
	return cs.genCode(true)
}

func (cs *ConverterSet) genCode(withPrelude bool) []byte {
	var b bytes.Buffer

	// Imports
	if len(cs.importPaths) > 0 {
		b.WriteString("import (\n")
		for _, imp := range slices.Sorted(maps.Keys(cs.importPaths)) {
			b.WriteString("\t" + packagePathToImportName(imp) + " " + `"` + imp + `"` + "\n")
		}
		b.WriteString(")\n")
	}

	// Prelude
	if withPrelude {
		b.WriteString(preludeCode)
		b.WriteString("\n")
	}

	// Type names
	b.WriteString("var typeLookup = map[string]map[string]string{}\n")
	if len(cs.namedTypes) > 0 {
		b.WriteString("func init() {\n")
		pkgs := map[string]struct{}{}
		for nt := range cs.namedTypes {
			pkgs[nt.pkg] = struct{}{}
		}

		for _, pkg := range slices.Sorted(maps.Keys(pkgs)) {
			b.WriteString("\t" + `typeLookup["` + pkg + `"] = map[string]string{}` + "\n")
		}

		typs := slices.SortedFunc(maps.Keys(cs.namedTypes), func(a, b namedType) int {
			if res := cmp.Compare(a.pkg, b.pkg); res != 0 {
				return res
			}
			return cmp.Compare(a.name, b.name)
		})

		for _, typ := range typs {
			var typStr string
			if typ.pkg != "" && typ.pkg != cs.basePkg {
				typStr += packagePathToImportName(typ.pkg) + "."
			}
			typStr += typ.name

			b.WriteString("\t" + `typeLookup["` + typ.pkg + `"]["` + typ.name + `"] = "` + typStr + `"` + "\n")
		}
		b.WriteString("}\n\n")

	}

	// Converter code
	{
		keys := slices.SortedFunc(maps.Keys(cs.convs), func(a, b convKey) int {
			if res := cmp.Compare(a.dir, b.dir); res != 0 {
				return res
			}
			return cmp.Compare(a.typString, b.typString)
		})

		for i, k := range keys {
			if i != 0 {
				b.WriteString("\n\n")
			}
			b.Write(cs.convs[k].code)
		}
	}

	return b.Bytes()
}
