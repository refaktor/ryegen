package typeset

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func parse(t *testing.T, code string) *types.Scope {
	t.Helper()

	require := require.New(t)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", code, parser.SkipObjectResolution|parser.ParseComments)
	require.NoError(err)
	conf := &types.Config{
		Context:   types.NewContext(),
		GoVersion: "go1.23",
	}
	pkg, err := conf.Check("", fset, []*ast.File{f}, nil)
	require.NoError(err)
	return pkg.Scope()
}

func TestTypeset(t *testing.T) {
	require := require.New(t)
	pkg := parse(t, `
package main

var A struct {
	X int
	Y string
	Z struct {
		P float32
		Q float64
	}
	W struct {}
}

type XType struct {
	A int
}

type XAlias = struct {
	A int
}
`)
	ts := New(nil)
	require.Equal( // struct hashing should go from inside to outside
		"struct_"+typeHash(
			"struct{X int; Y string; Z struct_"+
				typeHash("struct{P float32; Q float64}")+
				"; W struct_"+typeHash("struct{}")+"}",
		),
		ts.TypeString(pkg.Lookup("A").Type()),
	)
	require.Equal(
		"struct_cd287c024d4e0fc1",
		ts.TypeString(pkg.Lookup("A").Type().
			Underlying().(*types.Struct).Field(2).Type()),
	)
	require.Equal(
		"XType",
		ts.TypeString(pkg.Lookup("XType").Type()),
	)
	for range 64 { // make the cache fill a bit
		require.Equal(
			"struct_d60633ba75ff3b24",
			ts.TypeString(pkg.Lookup("XAlias").Type()),
		)
	}
	var aliases []string
	for a := range ts.Aliases() {
		aliases = append(aliases, a.Name)
	}
	require.Contains(aliases, "struct_2a6c3b5b31e647fb")
	require.Contains(aliases, "struct_d60633ba75ff3b24")
	require.Len(aliases, 4)
	require.Len(ts.nameCache, 9)
}

func TestNormalizeType(t *testing.T) {
	require := require.New(t)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", `
package main

type X[T any] int
func (*X[T]) Foo(int, T) {}

func Expect[T any](*X[T], int, T) {}

type FooFunc[T any] = func(*X[T], int, T)
var _ FooFunc[int] = (*X[int]).Foo

var emptyIface = interface{}(nil)

type MyInt = int
var myInt = []MyInt{0}

type Nested = struct {
	A interface{}
	B int
	C struct {
		X int
	}
}
`,
		parser.SkipObjectResolution|parser.ParseComments,
	)
	require.NoError(err)

	conf := &types.Config{
		Context:          types.NewContext(),
		GoVersion:        "go1.23",
		IgnoreFuncBodies: true,
	}
	pkg, err := conf.Check("", fset, []*ast.File{f}, nil)
	require.NoError(err)

	ts := New(nil)

	// Check alias resolution
	{
		typ := pkg.Scope().Lookup("myInt")
		require.Equal(
			"[]MyInt",
			typ.Type().String(),
		)
		require.Equal(
			"[]int",
			ts.Normalized(typ.Type()).String(),
		)
	}

	// Check receiver substitution (including with generics)
	{
		typeX := pkg.Scope().Lookup("X").Type().(*types.Named)
		funcFoo := typeX.Method(0)
		funcExpect := pkg.Scope().Lookup("Expect").(*types.Func)
		require.Equal(
			funcExpect.Signature().String(),
			ts.Normalized(funcFoo.Signature()).String(),
		)
	}

	// Check interface{} -> any
	{
		typ := pkg.Scope().Lookup("emptyIface")
		require.Equal(
			"interface{}",
			typ.Type().String(),
		)
		require.Equal(
			"any",
			ts.Normalized(typ.Type()).String(),
		)
	}
}
