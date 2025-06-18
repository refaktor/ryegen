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

var TMP_SUPERDEBUG = false

// Applies some transformations on a type before
// it gets passed to the converter.
func transformType(typ types.Type) (types.Type, error) {
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

	return walktypes.WalkModify(typ, transformType)
}

type ConverterSpec struct {
	typ types.Type
	dir Direction
	err error
}

func NewSpec(typ types.Type, dir Direction) ConverterSpec {
	// Transform type if necessary
	typ, err := transformType(typ)

	return ConverterSpec{
		typ: typ,
		dir: dir,
		err: err,
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
	if spec.err != nil {
		return "", nil, spec.err
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
		if typ.Key().String() == "string" {
			tmplName = "map"
			deps.Converters = append(deps.Converters,
				NewSpec(typ.Key(), spec.Dir()),
				NewSpec(typ.Elem(), spec.Dir()),
			)
		} else {
			tmplName = "named"
		}
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
