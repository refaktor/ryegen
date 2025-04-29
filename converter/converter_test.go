package converter_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/refaktor/ryegen/v2/converter"
	"github.com/stretchr/testify/require"
)

func TestConverter(t *testing.T) {
	// To Rye
	testConverter(t, "to_rye/integer_int.go", "int", converter.ToRye)
	testConverter(t, "to_rye/integer_byte.go", "byte", converter.ToRye)
	testConverter(t, "to_rye/integer_uint8.go", "uint8", converter.ToRye)
	testConverter(t, "to_rye/integer_int32.go", "int32", converter.ToRye)
	testConverter(t, "to_rye/float32.go", "float32", converter.ToRye)
	testConverter(t, "to_rye/float64.go", "float64", converter.ToRye)
	testConverter(t, "to_rye/string.go", "string", converter.ToRye)
	testConverter(t, "to_rye/error.go", "error", converter.ToRye)
	testConverter(t, "to_rye/map_01_basic.go", "map[string]int", converter.ToRye)
	testConverter(t, "to_rye/map_02_nonstring.go", "map[int]int", converter.ToRye)
	testConverter(t, "to_rye/func_01_no_params.go", "func()", converter.ToRye)
	testConverter(t, "to_rye/func_02_with_params.go", "func(a, b int, c string, d map[int]int)", converter.ToRye)
	testConverter(t, "to_rye/func_03_single_result.go", "func() string", converter.ToRye)
	testConverter(t, "to_rye/func_04_error_result.go", "func() (string, error)", converter.ToRye)
	testConverter(t, "to_rye/func_05_multiple_results.go", "func() (string, int, map[string]string)", converter.ToRye)
	testConverter(t, "to_rye/slice_01_basic.go", "[]int", converter.ToRye)
	testConverter(t, "to_rye/array_01_basic.go", "[69]int", converter.ToRye)

	// From Rye
	testConverter(t, "from_rye/integer_int.go", "int", converter.FromRye)
	testConverter(t, "from_rye/integer_uint16.go", "uint16", converter.FromRye)
	testConverter(t, "from_rye/integer_uint64.go", "uint64", converter.FromRye)
	testConverter(t, "from_rye/float32.go", "float32", converter.FromRye)
	testConverter(t, "from_rye/float64.go", "float64", converter.FromRye)
	testConverter(t, "from_rye/string.go", "string", converter.FromRye)
	testConverter(t, "from_rye/error.go", "error", converter.FromRye)
	testConverter(t, "from_rye/ptr_int.go", "*int", converter.FromRye)
	testConverter(t, "from_rye/slice_01_basic.go", "[]int", converter.FromRye)
	testConverter(t, "from_rye/array_01_basic.go", "[69]int", converter.FromRye)
	testConverter(t, "from_rye/func_01_no_params.go", "func()", converter.FromRye)
	testConverter(t, "from_rye/func_02_with_params.go", "func(a, b int, c string, d map[int]int)", converter.FromRye)
	testConverter(t, "from_rye/func_03_single_result.go", "func() string", converter.FromRye)
	testConverter(t, "from_rye/func_04_multiple_results.go", "func() (string, int, map[string]string)", converter.FromRye)
}

// If filename doesn't exist, the resulting converter will be written to the file
// without checking against anything. If filename does exist, the resulting
// converter will be checked against the contents of the file.
func testConverter(t *testing.T, filename, typeExpr string, dir converter.Direction) {
	t.Helper()
	t.Run(filepath.Base(strings.TrimSuffix(filename, ".go")+" "+dir.String()), func(t *testing.T) {
		doTestConverter(t, filename, typeExpr, dir)
	})
}

func doTestConverter(t *testing.T, filename, typeExpr string, dir converter.Direction) {
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
	got, _, err := converter.NewSpec(info.TypeOf(typExpr), dir).Generate()
	require.NoError(err)
	if info, err := os.Stat(filePath); err == nil && info.Mode().IsRegular() {
		expect, err := os.ReadFile(filePath)
		require.NoError(err)
		require.Equal(string(expect), got, "Test: \"%v\" %v -> %v", typeExpr, dir, filePath)
	} else {
		err := os.WriteFile(filePath, []byte(got), 0666)
		require.NoError(err)
	}
}
