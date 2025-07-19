package converter

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"go/types"
	"hash/fnv"
	"maps"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/refaktor/ryegen/v2/converter/walktypes"
)

var (
	ErrInternalPackage         = errors.New("use of internal package")
	ErrUnexported              = errors.New("use of unexported name")
	ErrGeneric                 = errors.New("use of generic declaration")
	ErrCGo                     = errors.New("use of CGo")
	ErrInvalid                 = errors.New("use of invalid type")
	ErrInterfaceTypeConstraint = errors.New("interface contains type constraint")
	ErrIncomplete              = errors.New("use of incomplete (or unallocatable) type")
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
		if tn.Pkg() != nil && tn.Pkg().Path() == "runtime/cgo" && tn.Name() == "Incomplete" {
			return ErrIncomplete
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
				return ErrInvalid
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
		case *types.Interface:
			if !t.IsMethodSet() {
				return ErrInterfaceTypeConstraint
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

		return walktypes.WalkErr(t, check)
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
func normalizeType(typ types.Type) types.Type {
	// Aliased types always behave exactly the same as the
	// type R in "type A = R", even if they're nested within
	// other types.
	typ = types.Unalias(typ)

	switch t := typ.(type) {
	case *types.Signature:
		if t.Recv() != nil {
			// Turn receiver into regular parameter for conversion.

			if t.TypeParams().Len() > 0 {
				// https://cs.opensource.google/go/go/+/master:src/go/types/signature.go;l=93;drc=b4309ece66ca989a38ed65404850a49ae8f92742
				panic("generic method cannot have any type params")
			}

			// Func signatures with a receiver can't have any
			// type params outside of their receiver, so transfer
			// receiver type params to new func body type params.
			var tParams []*types.TypeParam
			for tParam := range t.RecvTypeParams().TypeParams() {
				tParams = append(tParams, types.NewTypeParam(
					tParam.Obj(),
					tParam.Constraint(),
				))
			}

			typ = types.NewSignatureType(
				nil,
				nil,
				tParams,
				types.NewTuple(append(
					[]*types.Var{t.Recv()},
					slices.Collect(t.Params().Variables())...,
				)...),
				t.Results(),
				t.Variadic(),
			)
		}
	case *types.Interface:
		if t.NumMethods() == 0 {
			typ = types.Universe.Lookup("any").Type()
			return typ
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

func (a convKey) cmp(b convKey) int {
	return cmp.Or(
		cmp.Compare(a.dir, b.dir),
		cmp.Compare(a.typString, b.typString),
	)
}

type convInfo struct {
	key        convKey
	typ        types.Type
	debugNames []string
}

type convSpec struct {
	typ types.Type
	dir Direction
}

type ConverterSet struct {
	seedConvs   map[convKey]convInfo // see [makeConvGraph]
	tmplToRye   *template.Template
	tmplFromRye *template.Template
	basePkg     string

	// The following variables are part of template dependency
	// injection (which is kind of a hack). This allows arbitrary
	// values to be inserted into and extracted from template
	// funcs.
	// They have to be re-used constantly, so best don't touch them.
	newImportPaths []string
	newDeps        []convSpec
	canConvert     func(convInfo) bool
}

// NewConverterSet creates a new [ConverterSet].
// basePkg is the package path Ryegen was initiated
// in (usually "main").
func NewConverterSet(basePkg string) *ConverterSet {
	cs := &ConverterSet{
		seedConvs: map[convKey]convInfo{},
		basePkg:   basePkg,
	}

	// Set up template functions that use template dependency
	// injection
	funcs := maps.Clone(templateFuncMap)
	funcs["conv"] = func(typ types.Type, dir Direction) string {
		cs.newDeps = append(cs.newDeps, convSpec{typ, dir})
		return cs.convName(typ, dir)
	}
	funcs["canConv"] = func(typ types.Type, dir Direction) bool {
		typ = normalizeType(typ)
		key := convKey{typString: typ.String(), dir: dir}
		info := convInfo{key: key, typ: typ}
		return cs.canConvert(info)
	}
	funcs["objStr"] = func(obj types.Object) string {
		return types.ObjectString(
			obj,
			cs.ImportNameQualifier,
		)
	}
	funcs["typStr"] = func(t types.Type) (string, error) {
		var collectImports func(t types.Type)
		collectImports = func(t types.Type) {
			if t, ok := t.(*types.Named); ok {
				if pkg := t.Obj().Pkg(); pkg != nil {
					path := pkg.Path()
					if path != cs.basePkg {
						cs.newImportPaths = append(cs.newImportPaths, path)
					}
				}
			}
			walktypes.Walk(t, collectImports)
		}
		collectImports(t)
		return types.TypeString(
			t,
			cs.ImportNameQualifier,
		), nil
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
			// TODO: Fix and re-enable (also re-enable in to_rye template)
			/*case "time":
			if typ.Obj().Name() == "Time" {
				return "time", nil
			}*/
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
			panic("logic error: recv should have been placed into params")
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
// debugName is used for printing error messages and
// generating debug info, optional.
// Returns the name of the resulting converter function.
func (cs *ConverterSet) Add(typ types.Type, dir Direction, debugName string) (converterName string) {
	typ = normalizeType(typ)
	key := convKey{typString: typ.String(), dir: dir}
	info := convInfo{key: key, typ: typ}
	if prevInfo, ok := cs.seedConvs[key]; ok {
		info.debugNames = prevInfo.debugNames
	}
	if debugName != "" {
		info.debugNames = append(info.debugNames, debugName)
	}
	cs.seedConvs[key] = info
	return cs.convName(typ, dir)
}

// Code returns all of the generated converter Go code.
// If the returned error is a [ConverterError], the returned
// code is still valid, but the erroneous converters and any
// converters associated with them are not included. Use
// [ConverterError.IsUsable] to find out which converters
// are still usable.
func (cs *ConverterSet) Code() ([]byte, error) {
	return cs.genCode(true)
}

// DebugDOTCode generates DOT (graphviz) code
// representing the complete converter dependency
// graph.
// If nodeRe is nil, all nodes are included. If
// nodeRe is non-nil, all nodes depending on any
// matching nodes are included.
func (cs *ConverterSet) DebugDOTCode(nodeRe *regexp.Regexp) []byte {
	graph := cs.genGraph()
	return graph.generateDOTCode(nodeRe)
}

func (cs *ConverterSet) genGraph() convGraph {
	return makeConvGraph(
		slices.SortedFunc(maps.Values(cs.seedConvs), func(a, b convInfo) int { return a.key.cmp(b.key) }),
		func(ci convInfo, canConvert func(convInfo) bool) (_code []byte, _deps []convInfo, _importPaths []string, _err error) {
			cs.canConvert = canConvert
			defer func() {
				cs.canConvert = nil
			}()

			if err := checkConvertible(ci.typ); err != nil {
				return nil, nil, nil, err
			}

			var tmpl *template.Template
			switch ci.key.dir {
			case ToRye:
				tmpl = cs.tmplToRye
			case FromRye:
				tmpl = cs.tmplFromRye
			default:
				panic("invalid conversion direction")
			}
			tmplName, err := cs.templateName(ci.typ)
			if err != nil {
				return nil, nil, nil, err
			}
			tmpl = tmpl.Lookup(tmplName)
			if tmpl == nil {
				return nil, nil, nil, fmt.Errorf("no template to convert %v %v", tmplName, ci.key.dir)
			}
			code, deps, importPaths, err := cs.executeTemplate(tmpl, ci.typ)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("execute converter template for %v %v: %w", tmplName, ci.key.dir, err)
			}
			depInfos := make([]convInfo, len(deps))
			for i, dep := range deps {
				depInfos[i] = convInfo{
					key: convKey{
						typString: dep.typ.String(),
						dir:       dep.dir,
					},
					typ: dep.typ,
				}
			}
			return code, depInfos, importPaths, nil
		},
	)
}

func (cs *ConverterSet) genCode(withPrelude bool) ([]byte, error) {
	graph := cs.genGraph()

	var namedTypes []namedType
	var importPaths []string
	var convCode [][]byte
	{
		mNamedTypes := map[namedType]struct{}{}
		mImportPaths := map[string]struct{}{}
		mConvCode := map[convKey][]byte{}

		for key, node := range graph.nodes {
			var addNamedTypes func(typ types.Type)
			addNamedTypes = func(typ types.Type) {
				if typ, ok := typ.(*types.Named); ok {
					pkgPath := ""
					if pkg := typ.Obj().Pkg(); pkg != nil {
						pkgPath = pkg.Path()
					}
					mNamedTypes[namedType{
						pkgPath,
						typ.Obj().Name()}] = struct{}{}
				}
				walktypes.Walk(typ, addNamedTypes)
			}
			addNamedTypes(node.typ)

			for _, imp := range node.importPaths {
				mImportPaths[imp] = struct{}{}
			}

			mConvCode[key] = node.code
		}

		namedTypes = slices.SortedFunc(maps.Keys(mNamedTypes), func(a, b namedType) int {
			if res := cmp.Compare(a.pkg, b.pkg); res != 0 {
				return res
			}
			return cmp.Compare(a.name, b.name)
		})
		importPaths = slices.Sorted(maps.Keys(mImportPaths))
		convCode = make([][]byte, 0, len(mConvCode))
		for _, key := range slices.SortedFunc(maps.Keys(mConvCode), convKey.cmp) {
			convCode = append(convCode, mConvCode[key])
		}
	}

	var b bytes.Buffer

	// Imports
	if len(importPaths) > 0 {
		b.WriteString("import (\n")
		for _, imp := range importPaths {
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
	if len(namedTypes) > 0 {
		b.WriteString("func init() {\n")
		seenPkgs := map[string]struct{}{}
		for _, nt := range namedTypes {
			if _, ok := seenPkgs[nt.pkg]; !ok {
				b.WriteString("\t" + `typeLookup["` + nt.pkg + `"] = map[string]string{}` + "\n")
				seenPkgs[nt.pkg] = struct{}{}
			}

			var ntStr string
			if nt.pkg != "" && nt.pkg != cs.basePkg {
				ntStr += packagePathToImportName(nt.pkg) + "."
			}
			ntStr += nt.name
			b.WriteString("\t" + `typeLookup["` + nt.pkg + `"]["` + nt.name + `"] = "` + ntStr + `"` + "\n")
		}
		b.WriteString("}\n\n")

	}

	// Converter code
	{
		for i, code := range convCode {
			if i != 0 {
				b.WriteString("\n\n")
			}
			b.Write(code)
		}
	}

	return b.Bytes(), newConverterError(graph)
}
