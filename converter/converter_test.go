package converter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConverter(t *testing.T) {
	// To Rye
	testConverter(t, "to_rye/integer_int.go", "int", ToRye)
	testConverter(t, "to_rye/integer_byte.go", "byte", ToRye)
	testConverter(t, "to_rye/integer_uint8.go", "uint8", ToRye)
	testConverter(t, "to_rye/integer_int32.go", "int32", ToRye)
	testConverter(t, "to_rye/float32.go", "float32", ToRye)
	testConverter(t, "to_rye/float64.go", "float64", ToRye)
	testConverter(t, "to_rye/string.go", "string", ToRye)
	testConverter(t, "to_rye/error.go", "error", ToRye)
	testConverter(t, "to_rye/map_01_basic.go", "map[string]int", ToRye)
	testConverter(t, "to_rye/map_02_nonstring.go", "map[int]int", ToRye)
	testConverter(t, "to_rye/func_01_no_params.go", "func()", ToRye)
	testConverter(t, "to_rye/func_02_with_params.go", "func(a, b int, c string, d []string)", ToRye)
	testConverter(t, "to_rye/func_03_single_result.go", "func() string", ToRye)
	testConverter(t, "to_rye/func_04_error_result.go", "func() (string, error)", ToRye)
	testConverter(t, "to_rye/func_05_multiple_results.go", "func() (string, int, map[string]string)", ToRye)
	testConverter(t, "to_rye/slice_01_basic.go", "[]int", ToRye)
	testConverter(t, "to_rye/array_01_basic.go", "[69]int", ToRye)

	// From Rye
	testConverter(t, "from_rye/integer_int.go", "int", FromRye)
	testConverter(t, "from_rye/integer_uint16.go", "uint16", FromRye)
	testConverter(t, "from_rye/integer_uint64.go", "uint64", FromRye)
	testConverter(t, "from_rye/float32.go", "float32", FromRye)
	testConverter(t, "from_rye/float64.go", "float64", FromRye)
	testConverter(t, "from_rye/string.go", "string", FromRye)
	testConverter(t, "from_rye/error.go", "error", FromRye)
	testConverter(t, "from_rye/ptr_int.go", "*int", FromRye)
	testConverter(t, "from_rye/slice_01_basic.go", "[]int", FromRye)
	testConverter(t, "from_rye/array_01_basic.go", "[69]int", FromRye)
	testConverter(t, "from_rye/func_01_no_params.go", "func()", FromRye)
	testConverter(t, "from_rye/func_02_with_params.go", "func(a, b int, c string, d []int)", FromRye)
	testConverter(t, "from_rye/func_03_single_result.go", "func() string", FromRye)
	testConverter(t, "from_rye/func_04_multiple_results.go", "func() (string, int, []string)", FromRye)
	testConverter(t, "from_rye/any.go", "any", FromRye)
}

// If filename doesn't exist, the resulting converter will be written to the file
// without checking against anything. If filename does exist, the resulting
// converter will be checked against the contents of the file.
func testConverter(t *testing.T, filename, typeExpr string, dir Direction) {
	t.Helper()
	t.Run(filename, func(t *testing.T) {
		doTestConverter(t, filename, typeExpr, dir)
	})
}

func doTestConverter(t *testing.T, filename, typeExpr string, dir Direction) {
	require := require.New(t)
	filePath := path.Join("testdata", filename)
	require.NoError(os.MkdirAll(path.Dir(filePath), os.ModePerm))
	typExpr, err := parser.ParseExpr(typeExpr)
	require.NoError(err)
	info := types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
	err = types.CheckExpr(token.NewFileSet(), nil, token.NoPos, typExpr, &info)
	require.NoError(err)
	cs := NewConverterSet()
	_, err = cs.Add(info.TypeOf(typExpr), dir)
	require.NoError(err)
	got := cs.genCode(false)
	if info, err := os.Stat(filePath); err == nil && info.Mode().IsRegular() {
		expect, err := os.ReadFile(filePath)
		require.NoError(err)
		require.Equal(string(expect), string(got), "Test: \"%v\" %v -> %v", typeExpr, dir, filePath)
	} else {
		err := os.WriteFile(filePath, got, 0666)
		require.NoError(err)
	}
}
