package converter

import (
	"embed"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"text/template"
)

//go:embed templates/*
var templates embed.FS

// Prelude code required by generated converters.
const preludeCode = `import (
	_errors "errors"
	_fmt "fmt"
	_reflect "reflect"
	_strings "strings"

	_env "github.com/refaktor/rye/env"
	_evaldo "github.com/refaktor/rye/evaldo"
	_sync "sync"
)

// Force-use some packages so we don't have to track them.
var _ = _errors.ErrUnsupported
var _ = _evaldo.BuiltinNames
var _ _sync.Mutex

// Prints a string representation of v.
func objectType(ps *_env.ProgramState, v any) string {
	if v, ok := v.(_env.Object); ok {
		return v.Inspect(*ps.Idx)
	} else {
		s := "nil"
		if t := _reflect.TypeOf(v); t != nil {
			s = t.String()
		}
		return "[Non-object of type " + s + "]"
	}
}

// Attempts to look up the type of v. If the type is found, this
// function returns an _env.Native of that type, true. If v's type
// is not found in the lookup table, this function returns
// _env.Native{}, false.
func autoToNative(ps *_env.ProgramState, v any) (_ _env.Native, ok bool) {
	t := _reflect.TypeOf(v)
	if t == nil {
		return _env.Native{}, false
	}
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
	// Dynamically generated for dependency tracking.
	"conv": (func(typ types.Type, dir Direction) string)(nil),
	// Attempts to generate a converter for typ, then
	// returns whether that converter could be generated
	// without any direct or indirect errors.
	//
	// Dynamically generated to include necessary context.
	"canConv": (func(typ types.Type, dir Direction) bool)(nil),
	// Returns a canonical string form of a types.Type.
	// Invoking this function will mark the type as an
	// import dependency of the converter it was invoked from.
	//
	// Never put this into generated string quotes ("").
	// Always use the "quote" function below and string
	// concatenation instead, so quotes inside the type
	// string are properly escaped.
	// E.g. "type is: " + {{ typStr . | quote }}
	//
	// Dynamically generated for dependency tracking.
	"typStr": (func(typ types.Type) string)(nil),
	// Returns a unique string hash for the given type.
	// You MUST prefix this with a usage (e.g. iface_) and a
	// direction so it doesn't get mixed up with typHashes
	// for other purposes. Useful for when you want to declare
	// a global object related to a single data type function.
	//
	// Use in conjunction with the once function to avoid
	// duplication.
	//
	// Dynamically generated to include necessary context.
	"typHash": (func(typ types.Type) string)(nil),
	// Returns true exactly once when executed with the
	// same string. Useful for when different converters
	// depend on a single instance of a global object.
	//
	// Dynamically generated to include necessary context.
	"once": (func(string) bool)(nil),
	// Returns whether the concrete type of typ matches typStr (case-insensitive, e.g. interface).
	// See the types satisfying types.Type for the concrete options.
	"typIs": func(typStr string, typ types.Type) bool {
		t := reflect.TypeOf(typ)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		return strings.EqualFold(typStr, t.Name())
	},
	"newPointer": func(elem types.Type) *types.Pointer {
		return types.NewPointer(elem)
	},
	"newMethodSet": func(t types.Type) *types.MethodSet {
		return types.NewMethodSet(t)
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
	// Returns the names of all struct fields.
	"structFieldNames": func(struc *types.Struct) []string {
		names := make([]string, struc.NumFields())
		for i := range names {
			names[i] = struc.Field(i).Name()
		}
		return names
	},
	// Returns the names of all method set methods.
	"methodSetMethodNames": func(ms *types.MethodSet) []string {
		names := make([]string, ms.Len())
		for i := range names {
			names[i] = (ms.At(i).Obj().(*types.Func)).Name()
		}
		return names
	},
	// Flips the channel direction (or leaves it unchanged if bidirectional).
	"flipChanDir": func(ch *types.Chan) *types.Chan {
		switch ch.Dir() {
		case types.SendRecv:
			return ch
		case types.SendOnly:
			return types.NewChan(types.RecvOnly, ch.Elem())
		case types.RecvOnly:
			return types.NewChan(types.SendOnly, ch.Elem())
		default:
			panic("invalid channel direction")
		}
	},
	// Returns whether the channel type can send.
	"chanCanSend": func(ch *types.Chan) bool {
		return ch.Dir() == types.SendRecv || ch.Dir() == types.SendOnly
	},
	// Returns whether the channel type can receive.
	"chanCanRecv": func(ch *types.Chan) bool {
		return ch.Dir() == types.SendRecv || ch.Dir() == types.RecvOnly
	},
	// Removes the signature's receiver, if it has one.
	"stripSignatureRecv": func(sig *types.Signature) *types.Signature {
		if sig.Recv() == nil {
			return sig
		}
		return types.NewSignatureType(
			nil,
			slices.Collect(sig.RecvTypeParams().TypeParams()),
			nil, // function with receiver can't have type params: https://cs.opensource.google/go/go/+/master:src/go/types/signature.go;l=93;drc=b4309ece66ca989a38ed65404850a49ae8f92742
			sig.Params(),
			sig.Results(),
			sig.Variadic(),
		)
	},
	// Returns the Universe scope.
	"universe": func() *types.Scope {
		return types.Universe
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
	"join": func(sep string, elems []string) string {
		return strings.Join(elems, sep)
	},
	"quote": strconv.Quote,
}
