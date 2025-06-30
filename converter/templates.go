package converter

import (
	_ "embed"
	"fmt"
	"go/types"
	"slices"
	"strings"
	"text/template"
)

//go:embed to_rye.go.tmpl
var templateSrcToRye string

//go:embed from_rye.go.tmpl
var templateSrcFromRye string

// Prelude code required by generated converters.
const preludeCode = `import (
	_errors "errors"
	_fmt "fmt"
	_reflect "reflect"
	_strings "strings"

	_env "github.com/refaktor/rye/env"
	_evaldo "github.com/refaktor/rye/evaldo"
)

// Force-use "errors", "evaldo" packages so we don't have to track them.
var _ = _errors.ErrUnsupported
var _ = _evaldo.BuiltinNames

// Prints a string representation of v.
func objectType(ps *_env.ProgramState, v any) string {
	if v, ok := v.(_env.Object); ok {
		return v.Inspect(*ps.Idx)
	} else {
		return "[Non-object of type " + _reflect.TypeOf(v).String() + "]"
	}
}

// Attempts to look up the type of v. If the type is found, this
// function returns an _env.Native of that type, true. If v's type
// is not found in the lookup table, this function returns
// _env.Native{}, false.
func autoToNative(ps *_env.ProgramState, v any) (_ _env.Native, ok bool) {
	t := _reflect.TypeOf(v)
	nPtrs := 0 // level of indirection for the native's name
	for t.Kind() == _reflect.Pointer {
		nPtrs++
		t = t.Elem()
	}
	pkgEntries, ok := typeLookup[t.PkgPath()]
	if !ok {
		return _env.Native{}, false
	}
	entry, ok := pkgEntries[t.Name()]
	if !ok {
		return _env.Native{}, false
	}
	name := "go(" + _strings.Repeat("*", nPtrs) + entry + ")"
	return *_env.NewNative(ps.Idx, v, name), true
}

func showFunctionError(ps *_env.ProgramState, fn _env.Function, err error) {
	ps.FailureFlag = true
	_fmt.Printf("Error: from function %v %v: %v\n",
		fn.Spec.Series.PositionAndSurroundingElements(*ps.Idx),
		fn.Body.Series.PositionAndSurroundingElements(*ps.Idx),
		err,
	)
}

func isNil(obj _env.Object) bool {
	_, ok := obj.(_env.Void)
	return ok
}
`

var templateFuncMap = template.FuncMap{
	"toRye":   func() Direction { return ToRye },
	"fromRye": func() Direction { return FromRye },
	// Returns the converter function name for typ.
	// Invoking this function will mark the converter
	// as a dependency of the converter it was invoked
	// from.
	//
	// Dynamically generated for dependency tracking.,
	"conv": (func(typ types.Type, dir Direction) string)(nil),
	// Returns a canonical string form of a types.Object.
	"objStr": func(obj types.Object) string {
		return types.ObjectString(
			obj,
			PkgImportNameQualifier,
		)
	},
	// Returns a canonical string form of a types.Type.
	// Invoking this function will mark the type as an
	// import dependency of the converter it was invoked from.
	//
	// Dynamically generated for dependency tracking.
	"typStr": (func(typ types.Type) string)(nil),
	// Returns a unique string hash for the type and conversion
	// direction. You MUST prefix this with a usage (e.g. iface_)
	// so it doesn't get mixed up with convHashes for other purposes.
	// Useful for when you want to declare a global object
	// related to a single conversion function. Doesn't have
	// any side effects.
	"convHash": func(typ types.Type, dir Direction) string {
		return typeHash(typ.String()) + "_" + dir.StringCamelCase()
	},
	"isStruct": func(typ types.Type) bool {
		_, ok := typ.(*types.Struct)
		return ok
	},
	"isInterface": func(typ types.Type) bool {
		_, ok := typ.(*types.Interface)
		return ok
	},
	"newPointer": func(elem types.Type) *types.Pointer {
		return types.NewPointer(elem)
	},
	// Splits off the last result if it is of type error.
	"splitErrResult": func(results *types.Tuple) (res struct {
		NonErr *types.Tuple
		Err    *types.Var
	}) {
		if results.Len() > 0 {
			lastVar := results.At(results.Len() - 1)
			last := lastVar.Type()
			if last, ok := last.(*types.Named); ok {
				if last.Obj().Pkg() == nil && last.Obj().Name() == "error" {
					res.NonErr = types.NewTuple(slices.Collect(results.Variables())[:results.Len()-1]...)
					res.Err = lastVar
					return
				}
			}
		}
		res.NonErr = results
		return
	},
	// Returns a signature that is sig with all parameters renamed to
	// fmt.Sprintf("%v%v", prefix, argIndex), and with all return
	// values named "_".
	"convFromRyeFuncHead": func(paramPrefix string, sig *types.Signature) *types.Signature {
		params := make([]*types.Var, sig.Params().Len())
		for i := range sig.Params().Len() {
			v := sig.Params().At(i)
			params[i] = types.NewVar(v.Pos(), v.Pkg(),
				fmt.Sprintf("%v%v", paramPrefix, i), v.Type())
		}
		results := make([]*types.Var, sig.Results().Len())
		for i := range sig.Results().Len() {
			v := sig.Results().At(i)
			results[i] = types.NewVar(v.Pos(), v.Pkg(), "_", v.Type())
		}
		return types.NewSignatureType(
			sig.Recv(),
			slices.Collect(sig.RecvTypeParams().TypeParams()),
			slices.Collect(sig.TypeParams().TypeParams()),
			types.NewTuple(params...),
			types.NewTuple(results...),
			sig.Variadic(),
		)
	},

	//
	// Miscellaneous utility functions
	//
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
	// Counts up to n-1, prepending pfx to
	// every item.
	// E.g. seqWithPrefix(3, "x") = ["x0" "x1" "x2"].
	"seqWithPrefix": func(n int, pfx string) []string {
		res := make([]string, n)
		for i := range n {
			res[i] = fmt.Sprintf("%v%v", pfx, i)
		}
		return res
	},
	"join": strings.Join,
}
