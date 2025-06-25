package converter

import (
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"hash/fnv"
	"maps"
	"slices"
	"strings"
	"text/template"

	"github.com/refaktor/ryegen/v2/converter/walktypes"
)

var (
	ErrInternalPackage error = errors.New("use of internal package")
	ErrUnexported      error = errors.New("use of unexported name")
	ErrGeneric         error = errors.New("use of generic declaration")
)

type Direction uint8

const (
	ToRye Direction = iota
	FromRye
)

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

func typeUniqueName(typ types.Type) string {
	switch typ := typ.(type) {
	case *types.Basic:
		return typ.Name()
	case *types.Pointer:
		return fmt.Sprintf("ptr_%v", typeUniqueName(typ.Elem()))
	case *types.Named:
		if typ.Obj().Pkg() != nil {
			return fmt.Sprintf("%v_%v", PkgImportNameQualifier(typ.Obj().Pkg()), typ.Obj().Name())
		} else {
			return typ.Obj().Name()
		}
	case *types.Signature:
		return fmt.Sprintf("func_%v", typeHash(typ.String()))
	case *types.Map:
		return fmt.Sprintf("map_%v_%v", typeUniqueName(typ.Key()), typeUniqueName(typ.Elem()))
	case *types.Array:
		return fmt.Sprintf("array_%v_%v", typ.Len(), typeUniqueName(typ.Elem()))
	case *types.Slice:
		return fmt.Sprintf("slice_%v", typeUniqueName(typ.Elem()))
	case *types.Struct:
		return fmt.Sprintf("struct_%v", typeHash(typ.String()))
	case *types.Interface:
		if typ.NumMethods() == 0 {
			return "any"
		}
		return fmt.Sprintf("interface_%v", typeHash(typ.String()))
	default:
		return fmt.Sprintf("unk_%v", typeHash(typ.String()))
	}
}

func convName(typ types.Type, dir Direction) string {
	var dirStr string
	switch dir {
	case ToRye:
		dirStr = "toRye"
	case FromRye:
		dirStr = "fromRye"
	default:
		panic("invalid conversion direction")
	}
	return fmt.Sprintf("conv_%v_%v", typeUniqueName(typ), dirStr)
}

// Returns an error if:
//   - Any internal or unexported component is required
//     to express the type in its Go representation
//   - The type uses any generics
func checkConvertible(typ types.Type) error {
	checkPkg := func(pkg *types.Package) error {
		if pkg == nil {
			return nil
		}
		for sp := range strings.SplitSeq(pkg.Path(), "/") {
			if sp == "internal" {
				return ErrInternalPackage
			}
		}
		return nil
	}
	checkVar := func(v *types.Var) error {
		if !ast.IsExported(v.Name()) {
			return ErrUnexported
		}
		return checkPkg(v.Pkg())
	}
	checkTypeName := func(tn *types.TypeName) error {
		if tn.Pkg() == nil {
			return nil
		}
		if !ast.IsExported(tn.Name()) {
			return ErrUnexported
		}
		return checkPkg(tn.Pkg())
	}

	stack := []types.Type{}
	var check func(t types.Type) error
	check = func(t types.Type) error {
		if slices.Contains(stack, typ) {
			// Break recursion loops
			return nil
		}
		stack = append(stack, typ)
		defer func() {
			stack = (stack)[:len(stack)-1]
		}()

		switch t := t.(type) {
		case *types.Alias:
			if t.TypeParams() != nil {
				return ErrGeneric
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

		return walktypes.Walk(typ, check)
	}

	return check(typ)
}

// e.g. github.com/someone/somerepo => github_com_someone_somerepo
func packagePathToImportName(path string) string {
	return strings.NewReplacer(
		"/", "_",
		".", "_",
		"-", "_",
	).Replace(path)
}

var PkgImportNameQualifier types.Qualifier = func(p *types.Package) string {
	return packagePathToImportName(p.Path())
}

var TMP_SUPERDEBUG = false

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
			if TMP_SUPERDEBUG {
				fmt.Println("sig type before:", typ)
			}
			// Turn receiver into regular parameter for conversion.
			params := append([]*types.Var{t.Recv()}, slices.Collect(t.Params().Variables())...)
			if t.RecvTypeParams() != nil || t.TypeParams() != nil {
				return nil, ErrGeneric
			}
			//typeParams := append(slices.Collect(t.RecvTypeParams().TypeParams()), slices.Collect(t.TypeParams().TypeParams())...)
			typ = types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), t.Results(), t.Variadic())
			if TMP_SUPERDEBUG {
				fmt.Println("sig type after:", typ)
			}
		}
	case *types.Interface:
		if t.NumMethods() == 0 {
			typ = types.NewNamed(types.NewTypeName(token.NoPos, nil, "any", nil), t, nil)
		}
	}

	return walktypes.WalkModify(typ, normalizeType)
}

type convSpec struct {
	typ types.Type
	dir Direction
}

type ConverterSet struct {
	code        map[convSpec]string
	importPaths map[string]struct{}
	tmplToRye   *template.Template
	tmplFromRye *template.Template

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

func NewConverterSet() *ConverterSet {
	cs := &ConverterSet{
		code:        map[convSpec]string{},
		importPaths: map[string]struct{}{},
	}

	// Set up template functions with dependency tracking
	funcs := maps.Clone(templateFuncMap)
	funcs["conv"] = func(typ types.Type, dir Direction) string {
		cs.newDeps = append(cs.newDeps, convSpec{typ, dir})
		return convName(typ, dir)
	}
	funcs["typStr"] = func(typ types.Type) string {
		if typ, ok := typ.(*types.Named); ok {
			if pkg := typ.Obj().Pkg(); pkg != nil {
				cs.newImportPaths = append(cs.newImportPaths, pkg.Path())
			}
		}
		return types.TypeString(
			typ,
			PkgImportNameQualifier,
		)
	}

	cs.tmplToRye = template.Must(template.New("to_rye.tmpl").Funcs(funcs).Parse(templateSrcToRye))
	cs.tmplFromRye = template.Must(template.New("from_rye.tmpl").Funcs(funcs).Parse(templateSrcFromRye))

	return cs
}

// Returns the name of the template used for the given type.
func (cs *ConverterSet) templateName(typ types.Type) (string, error) {
	switch typ := typ.(type) {
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
			case "any":
				return "any", nil
			}
		case "time":
			if typ.Obj().Name() == "Time" {
				return "time", nil
			}
		}
		return "named", nil
	case *types.Map:
		if typ.Key().String() == "string" {
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
func (cs *ConverterSet) executeTemplate(tmpl *template.Template, data types.Type) (code string, deps []convSpec, importPaths []string, err error) {
	defer func() {
		cs.newDeps = cs.newDeps[:0]
		cs.newImportPaths = cs.newImportPaths[:0]
	}()

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", nil, nil, err
	}

	// New first-order converter dependencies and
	// imports have been collected in newDeps/newImports
	// by the template execution.
	deps = slices.Clone(cs.newDeps)
	importPaths = slices.Clone(cs.newImportPaths)

	return b.String(), deps, importPaths, nil
}

// Assumes typ to already be normalized and checked for convertibility.
func (cs *ConverterSet) addNormalizedAndChecked(typ types.Type, dir Direction) error {
	if _, alreadyAdded := cs.code[convSpec{typ, dir}]; alreadyAdded {
		return nil
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
		return err
	}
	tmpl = tmpl.Lookup(tmplName)
	if tmpl == nil {
		return fmt.Errorf("no template to convert %v %v", tmplName, dir)
	}
	code, deps, importPaths, err := cs.executeTemplate(tmpl, typ)
	if err != nil {
		return fmt.Errorf("execute converter template for %v %v: %w", tmplName, dir, err)
	}

	cs.code[convSpec{typ, dir}] = code
	for _, dep := range deps {
		if dep.typ != typ || dep.dir != dir {
			// TODO: If adding the sub-converter fails, import dependencies
			// will be added to the ConverterSet, even though they may
			// technically fall away due to errors.
			// The best solution would probably be to track all converters,
			// the converters they depend on and the imports they depend on;
			// and then prune all orphaned imports.
			if err := cs.addNormalizedAndChecked(dep.typ, dep.dir); err != nil {
				return err
			}
		}
	}
	for _, imp := range importPaths {
		cs.importPaths[imp] = struct{}{}
	}
	return nil
}

func (cs *ConverterSet) Add(typ types.Type, dir Direction) error {
	typ, err := normalizeType(typ)
	if err != nil {
		return err
	}
	if err := checkConvertible(typ); err != nil {
		return err
	}
	return cs.addNormalizedAndChecked(typ, dir)
}

// Code returns all of the generated converter code.
func (cs *ConverterSet) Code() string {
	return cs.genCode(true)
}

func (cs *ConverterSet) genCode(withPrelude bool) string {
	var b strings.Builder

	// Imports
	if len(cs.importPaths) > 0 {
		b.WriteString("import (\n")
		for _, imp := range slices.Sorted(maps.Keys(cs.importPaths)) {
			b.WriteString("\t" + packagePathToImportName(imp) + " " + `"` + imp + `"`)
		}
		b.WriteString(")\n")
	}

	// Prelude
	if withPrelude {
		b.WriteString(PreludeCode)
		b.WriteString("\n")
	}

	// Converter code
	{
		keys := slices.SortedFunc(maps.Keys(cs.code), func(a, b convSpec) int {
			if res := cmp.Compare(a.dir, b.dir); res != 0 {
				return res
			}
			return cmp.Compare(a.typ.String(), b.typ.String())
		})

		for i, k := range keys {
			if i != 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(cs.code[k])
		}
	}

	return b.String()
}
