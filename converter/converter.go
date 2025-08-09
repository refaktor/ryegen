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

	"github.com/refaktor/ryegen/v2/converter/typeset"
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

type convKey struct {
	typString string
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

	tset  *typeset.TypeSet
	onces map[string]struct{} // see "once" in [templateFuncMap]

	// The following variables are part of template dependency
	// injection (which is kind of a hack). This allows arbitrary
	// values to be inserted into and extracted from template
	// funcs.
	// They have to be re-used constantly, so best don't touch them.
	newImports []*types.Package
	newDeps    []convSpec
	canConvert func(convInfo) bool
}

// NewConverterSet creates a new [ConverterSet].
// basePkg is the package path Ryegen was initiated
// in (usually "main").
func NewConverterSet(tset *typeset.TypeSet, basePkg string) *ConverterSet {
	cs := &ConverterSet{
		seedConvs: map[convKey]convInfo{},
		basePkg:   basePkg,
		tset:      tset,
		onces:     map[string]struct{}{},
	}

	// Set up template functions that use template dependency
	// injection
	funcs := maps.Clone(templateFuncMap)
	funcs["conv"] = func(typ types.Type, dir Direction) string {
		typ = cs.tset.Normalized(typ)
		cs.newDeps = append(cs.newDeps, convSpec{typ, dir})
		return cs.convName(typ, dir)
	}
	funcs["canConv"] = func(typ types.Type, dir Direction) bool {
		key := convKey{typString: cs.tset.TypeString(typ), dir: dir}
		info := convInfo{key: key, typ: typ}
		return cs.canConvert(info)
	}
	funcs["typStr"] = func(t types.Type) (string, error) {
		var collectImports func(t types.Type)
		collectImports = func(t types.Type) {
			switch t := t.(type) {
			case *types.Named:
				if t.Obj().Exported() {
					if pkg := t.Obj().Pkg(); pkg != nil {
						if pkg.Path() != cs.basePkg {
							cs.newImports = append(cs.newImports, pkg)
						}
					}
				}
			case *types.Basic:
				if t.Kind() == types.UnsafePointer {
					cs.newImports = append(cs.newImports, types.Unsafe)
				}
			}
			walktypes.Walk(t, collectImports)
		}
		collectImports(t)
		return cs.tset.TypeString(t), nil
	}
	funcs["typHash"] = func(typ types.Type) string {
		return typeHash(cs.tset.TypeString(typ))
	}
	funcs["once"] = func(s string) bool {
		if _, seen := cs.onces[s]; seen {
			return false
		} else {
			cs.onces[s] = struct{}{}
			return true
		}
	}

	cs.tmplToRye = template.Must(template.New("to_rye.go.tmpl").Funcs(funcs).
		ParseFS(templates, "templates/common.go.tmpl", "templates/to_rye.go.tmpl"))
	cs.tmplFromRye = template.Must(template.New("from_rye.go.tmpl").Funcs(funcs).
		ParseFS(templates, "templates/common.go.tmpl", "templates/from_rye.go.tmpl"))

	return cs
}

func (cs *ConverterSet) typeUniqueName(typ types.Type) string {
	switch typ := typ.(type) {
	case *types.Alias:
		if typ.Obj().Name() == "any" && typ.Obj().Parent() == types.Universe {
			return "any"
		}
		if cs.tset.ContainsAlias(typ) {
			return typ.Obj().Name()
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
			return fmt.Sprintf("%v_%v", cs.tset.Qualifier()(typ.Obj().Pkg()), typ.Obj().Name())
		} else {
			return typ.Obj().Name()
		}
	case *types.Signature:
		return fmt.Sprintf("func_%v", typeHash(cs.tset.TypeString(typ)))
	case *types.Map:
		return fmt.Sprintf("map_%v_%v", cs.typeUniqueName(typ.Key()), cs.typeUniqueName(typ.Elem()))
	case *types.Array:
		return fmt.Sprintf("array_%v_%v", typ.Len(), cs.typeUniqueName(typ.Elem()))
	case *types.Slice:
		return fmt.Sprintf("slice_%v", cs.typeUniqueName(typ.Elem()))
	case *types.Struct:
		return fmt.Sprintf("struct_%v", typeHash(cs.tset.TypeString(typ)))
	case *types.Interface:
		return fmt.Sprintf("interface_%v", typeHash(cs.tset.TypeString(typ)))
	case *types.Chan:
		dirName := [...]string{
			types.SendRecv: "sr",
			types.SendOnly: "s",
			types.RecvOnly: "r",
		}
		return fmt.Sprintf("chan_%v_%v", dirName[typ.Dir()], cs.typeUniqueName(typ.Elem()))
	}
	return fmt.Sprintf("unk_%v", typeHash(cs.tset.TypeString(typ)))
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
		case typ.Info()&types.IsComplex != 0:
			return "complex", nil
		case typ.Info()&types.IsFloat != 0:
			return "float", nil
		case typ.Info()&types.IsBoolean != 0:
			return "bool", nil
		case typ.Info()&types.IsString != 0:
			return "string", nil
		}
		if typ.Kind() == types.UnsafePointer {
			return "unsafePointer", nil
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
			panic("logic error: recv should have been placed into params")
		}
		return "func", nil
	case *types.Array:
		return "array", nil
	case *types.Slice:
		return "slice", nil
	case *types.Struct:
		return "struct", nil
	case *types.Chan:
		return "chan", nil
	}
	return "", fmt.Errorf("no known converter template for type %v", typ)
}

// executeTemplate executes the converter template tmpl on data and
// returns the generated code, and the collected converter dependencies
// and import dependencies.
func (cs *ConverterSet) executeTemplate(tmpl *template.Template, data types.Type) (code []byte, deps []convSpec, imports []*types.Package, err error) {
	defer func() {
		cs.newDeps = cs.newDeps[:0]
		cs.newImports = cs.newImports[:0]
	}()

	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, nil, nil, err
	}

	// New first-order converter dependencies and
	// imports have been collected in newDeps/newImports
	// by the template execution.
	deps = slices.Clone(cs.newDeps)
	imports = slices.Clone(cs.newImports)

	return b.Bytes(), deps, imports, nil
}

// Add adds a converter to the ConverterSet, meaning it will end up
// in the generated code.
// debugName is used for printing error messages and
// generating debug info, optional.
// Returns the name of the resulting converter function.
func (cs *ConverterSet) Add(typ types.Type, dir Direction, debugName string) (converterName string) {
	typ = cs.tset.Normalized(typ)
	key := convKey{typString: cs.tset.TypeString(typ), dir: dir}
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
// The returned [Graph] can be used to get information
// about the resulting converter graph.
// Call [Graph.Contains] to find out which converters are
// still usable.
// If the returned error is a [ConverterError], the returned
// code is still valid, but the erroneous converters and any
// converters associated with them are not included. Call
// [ConverterError.String] to get a detailed list of all
// errors.
func (cs *ConverterSet) Code() ([]byte, *Graph, error) {
	return cs.genCode(true)
}

func (cs *ConverterSet) genGraph() convGraph {
	return makeConvGraph(
		slices.SortedFunc(maps.Values(cs.seedConvs), func(a, b convInfo) int { return a.key.cmp(b.key) }),
		func(ci convInfo, canConvert func(convInfo) bool) (_code []byte, _deps []convInfo, _imports []*types.Package, _err error) {
			cs.canConvert = canConvert
			defer func() {
				cs.canConvert = nil
			}()

			typ := ci.typ
			if cs.tset.ContainsAlias(typ) {
				// We don't want a struct converter to deal with the aliased type
				typ = typ.Underlying()
			}

			if err := checkConvertible(typ); err != nil {
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
			tmplName, err := cs.templateName(typ)
			if err != nil {
				return nil, nil, nil, err
			}
			tmpl = tmpl.Lookup(tmplName)
			if tmpl == nil {
				return nil, nil, nil, fmt.Errorf("no template to convert %v %v", tmplName, ci.key.dir)
			}
			code, deps, importPaths, err := cs.executeTemplate(tmpl, typ)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("execute converter template for %v %v: %w", tmplName, ci.key.dir, err)
			}
			depInfos := make([]convInfo, len(deps))
			for i, dep := range deps {
				depInfos[i] = convInfo{
					key: convKey{
						typString: cs.tset.TypeString(dep.typ),
						dir:       dep.dir,
					},
					typ: dep.typ,
				}
			}
			return code, depInfos, importPaths, nil
		},
	)
}

func (cs *ConverterSet) genCode(withPrelude bool) ([]byte, *Graph, error) {
	graph := cs.genGraph()

	var namedTypes []*types.TypeName
	var imports []*types.Package
	convCode := map[convKey][]byte{}
	{
		for key, node := range graph.nodes {
			var addNamedTypes func(typ types.Type)
			addNamedTypes = func(typ types.Type) {
				if typ, ok := typ.(*types.Named); ok {
					namedTypes = append(namedTypes, typ.Obj())
				}
				walktypes.Walk(typ, addNamedTypes)
			}
			addNamedTypes(node.typ)

			imports = append(imports, node.imports...)

			convCode[key] = node.code
		}
		namedTypes = sortedUniq(namedTypes, func(a, b *types.TypeName) int {
			return cmp.Or(cmpPkgs(a.Pkg(), b.Pkg()),
				cmp.Compare(a.Name(), b.Name()))
		})
		imports = sortedUniq(imports, cmpPkgs)
	}

	var b bytes.Buffer

	// Imports
	if len(imports) > 0 {
		b.WriteString("import (\n")
		for _, imp := range imports {
			b.WriteString("\t" + cs.tset.Qualifier()(imp) + " " + `"` + imp.Path() + `"` + "\n")
		}
		b.WriteString(")\n")
	}

	// Prelude
	if withPrelude {
		b.WriteString(preludeCode)
		b.WriteString("\n")
	}

	// Struct alias declarations
	{
		found := false
		for alias := range cs.tset.Aliases() {
			typStr := cs.tset.TypeString(alias.Type)
			_, ok := graph.nodes[convKey{typString: typStr, dir: ToRye}]
			if !ok {
				_, ok = graph.nodes[convKey{typString: typStr, dir: FromRye}]
			}
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "type %v = %v\n", alias.Name, types.TypeString(alias.Type, cs.tset.Qualifier()))
			found = true
		}
		if found {
			b.WriteString("\n")
		}
	}

	// Type names
	b.WriteString("var typeLookup = map[string]map[string]string{}\n")
	if len(namedTypes) > 0 {
		b.WriteString("func init() {\n")
		seenPkgs := map[string]struct{}{}
		for _, nt := range namedTypes {
			pkg := ""
			if nt.Pkg() != nil {
				pkg = nt.Pkg().Path()
			}
			if _, ok := seenPkgs[pkg]; !ok {
				b.WriteString("\t" + `typeLookup["` + pkg + `"] = map[string]string{}` + "\n")
				seenPkgs[pkg] = struct{}{}
			}

			var ntStr string
			if pkg != "" && pkg != cs.basePkg {
				ntStr += cs.tset.Qualifier()(nt.Pkg()) + "."
			}
			ntStr += nt.Name()
			b.WriteString("\t" + `typeLookup["` + pkg + `"]["` + nt.Name() + `"] = "` + ntStr + `"` + "\n")
		}
		b.WriteString("}\n\n")

	}

	// Converter code
	{
		for i, key := range slices.SortedFunc(maps.Keys(convCode), convKey.cmp) {
			code := convCode[key]
			if i != 0 {
				b.WriteString("\n\n")
			}
			b.Write(code)
		}
	}

	return b.Bytes(), newGraph(graph, cs.tset), newConverterError(graph)
}
