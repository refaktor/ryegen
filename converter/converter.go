package converter

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"hash/fnv"
	"slices"
	"strings"
	"text/template"
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

// Returns an error if:
//   - Any internal or unexported component is required
//     to express the type in its Go representation
//   - The type uses any generics
func checkConvertible(typ types.Type) error {
	stack := &[]types.Type{}

	var check func(typ types.Type) error
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
		if err := check(v.Type()); err != nil {
			return err
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
	check = func(typ types.Type) error {
		if slices.Contains(*stack, typ) {
			// Break recursion loops
			return nil
		}
		*stack = append(*stack, typ)
		defer func() {
			*stack = (*stack)[:len(*stack)-1]
		}()
		switch typ := typ.(type) {
		case *types.Basic:
			return nil
		case *types.Named:
			if typ.TypeParams() != nil {
				return ErrGeneric
			}
			return checkTypeName(typ.Obj())
		case *types.Alias:
			if typ.TypeParams() != nil {
				return ErrGeneric
			}
			return check(typ.Rhs())
		case *types.Array:
			return check(typ.Elem())
		case *types.Chan:
			return check(typ.Elem())
		case *types.Pointer:
			return check(typ.Elem())
		case *types.Slice:
			return check(typ.Elem())
		case *types.TypeParam:
			return ErrGeneric
		case *types.Map:
			if err := check(typ.Key()); err != nil {
				return err
			}
			if err := check(typ.Elem()); err != nil {
				return err
			}
			return nil
		case *types.Signature:
			if typ.TypeParams() != nil || typ.RecvTypeParams() != nil {
				return ErrGeneric
			}
			for v := range typ.Params().Variables() {
				if err := check(v.Type()); err != nil {
					return err
				}
			}
			for v := range typ.Results().Variables() {
				if err := check(v.Type()); err != nil {
					return err
				}
			}
			if v := typ.Recv(); v != nil {
				if err := check(v.Type()); err != nil {
					return err
				}
			}
			return nil
		case *types.Struct:
			for v := range typ.Fields() {
				if err := checkVar(v); err != nil {
					return err
				}
			}
			return nil
		case *types.Interface:
			for v := range typ.ExplicitMethods() {
				if err := check(v.Signature()); err != nil {
					return err
				}
			}
			for v := range typ.EmbeddedTypes() {
				if err := check(v); err != nil {
					return err
				}
			}
			return nil
		}
		panic(fmt.Sprintf("unhandled type %T", typ))
	}
	return check(typ)
}

// e.g. github.com/someone/somerepo => github_com_someone_somerepo
var PkgImportNameQualifier types.Qualifier = func(p *types.Package) string {
	return strings.NewReplacer(
		"/", "_",
		".", "_",
		"-", "_",
	).Replace(p.Path())
}

type ConverterDependencies struct {
	Converters []ConverterSpec
	Imports    []*types.Package
}

type ConverterSpec struct {
	typ         types.Type
	dir         Direction
	hasGenerics bool
}

func NewSpec(typ types.Type, dir Direction) ConverterSpec {
	hasGenerics := false

	// Transform type if necessary
	{
		switch t := typ.(type) {
		case *types.Signature:
			if t.Recv() != nil {
				// Turn receiver into regular parameter for conversion.
				params := append([]*types.Var{t.Recv()}, slices.Collect(t.Params().Variables())...)
				if t.RecvTypeParams() != nil || t.TypeParams() != nil {
					hasGenerics = true
				}
				//typeParams := append(slices.Collect(t.RecvTypeParams().TypeParams()), slices.Collect(t.TypeParams().TypeParams())...)
				typ = types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), t.Results(), t.Variadic())
			}
		}
		// Aliased types always behave exactly the same as the
		// type R in "type A = R", even if they're nested within
		// other types.
		typ = types.Unalias(typ)
		// interface{} == any
		if i, ok := typ.(*types.Interface); ok && i.NumMethods() == 0 {
			typ = types.NewNamed(types.NewTypeName(token.NoPos, nil, "any", nil), i, nil)
		}
	}

	return ConverterSpec{
		typ:         typ,
		dir:         dir,
		hasGenerics: hasGenerics,
	}
}

// Returns the converter name, i.e. name of the conversion
// function generated when calling Generate.
func (spec ConverterSpec) Name() string {
	var dirStr string
	switch spec.dir {
	case ToRye:
		dirStr = "toRye"
	case FromRye:
		dirStr = "fromRye"
	default:
		panic("invalid conversion direction")
	}
	return fmt.Sprintf("conv_%v_%v", typeUniqueName(spec.typ), dirStr)
}

// Returns the effective converter type. It may have been
// transformed previously (e.g. func receiver is moved into params).
func (spec ConverterSpec) Type() types.Type {
	return spec.typ
}

func (spec ConverterSpec) Dir() Direction {
	return spec.dir
}

// Generates the conversion function.
// dependencies are the converters this one depends on
// and should be generated and deduplicated by the caller.
func (spec ConverterSpec) Generate() (text string, deps *ConverterDependencies, err error) {
	if spec.hasGenerics {
		return "", nil, ErrGeneric
	}
	if err := checkConvertible(spec.Type()); err != nil {
		return "", nil, err
	}

	deps = &ConverterDependencies{}

	var tmpl *template.Template
	switch spec.Dir() {
	case ToRye:
		tmpl = templateToRye
	case FromRye:
		tmpl = templateFromRye
	default:
		panic("invalid conversion direction")
	}

	var tmplName string
	switch typ := spec.Type().(type) {
	case *types.Basic:
		switch typ.Kind() {
		case types.Int, types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Int8, types.Int16, types.Int32, types.Int64:
			tmplName = "integer"
		case types.Float32, types.Float64:
			tmplName = "float"
		case types.Bool:
			tmplName = "bool"
		case types.String:
			tmplName = "string"
		}
	case *types.Pointer:
		tmplName = "pointer"
		deps.Converters = append(deps.Converters,
			NewSpec(typ.Elem(), spec.Dir()),
		)
	case *types.Named:
		tmplName = "named"
		var pkgPath string
		if typ.Obj().Pkg() != nil {
			pkgPath = typ.Obj().Pkg().Path()
		}
		switch pkgPath {
		case "":
			switch typ.Obj().Name() {
			case "error":
				tmplName = "error"
			case "any":
				tmplName = "any"
			}
		case "time":
			if typ.Obj().Name() == "Time" {
				tmplName = "time"
			}
		}
		if pkg := typ.Obj().Pkg(); pkg != nil {
			deps.Imports = append(deps.Imports,
				pkg,
			)
		}
	case *types.Map:
		tmplName = "map"
		deps.Converters = append(deps.Converters,
			NewSpec(typ.Key(), spec.Dir()),
			NewSpec(typ.Elem(), spec.Dir()),
		)
	case *types.Signature:
		tmplName = "func"
		if typ.Recv() != nil {
			panic("logic error: recv sholuld have been placed into params")
		}
		params := typ.Params()
		if params != nil {
			for v := range params.Variables() {
				deps.Converters = append(deps.Converters,
					NewSpec(v.Type(), spec.Dir().Opposite()),
				)
			}
		}
		results := typ.Results()
		if results != nil {
			for v := range results.Variables() {
				deps.Converters = append(deps.Converters,
					NewSpec(v.Type(), spec.Dir()),
				)
			}
		}
	case *types.Array:
		tmplName = "array"
		deps.Converters = append(deps.Converters,
			NewSpec(typ.Elem(), spec.Dir()),
		)
	case *types.Slice:
		tmplName = "slice"
		deps.Converters = append(deps.Converters,
			NewSpec(typ.Elem(), spec.Dir()),
		)
	case *types.Struct:
		tmplName = "struct"
		for f := range typ.Fields() {
			deps.Converters = append(deps.Converters,
				NewSpec(f.Type(), spec.Dir()),
			)
		}
	}
	if tmplName == "" {
		return "", nil, fmt.Errorf("cannot convert %v", spec.Type())
	}

	tmpl = tmpl.Lookup(tmplName)
	if tmpl == nil {
		return "", nil, fmt.Errorf("no converter to convert %v %v", tmplName, spec.Dir())
	}
	var w strings.Builder
	if err := tmpl.Execute(&w, spec.Type()); err != nil {
		return "", nil, err
	}
	return w.String(), deps, nil
}
