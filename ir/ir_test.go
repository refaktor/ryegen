package ir_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/refaktor/ryegen/ir"
)

func parseSingleFile(t *testing.T, path string) *ir.IR {
	t.Helper()

	fileRd, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(
		token.NewFileSet(),
		filepath.Base(path),
		fileRd,
		parser.SkipObjectResolution|parser.ParseComments,
	)
	if err != nil {
		t.Fatal(err)
	}
	modNames := ir.UniqueModuleNames{"test.module/tm": "testmodule"}
	modDefaultNames := map[string]string{"test.module/tm": "testmodule"}
	input := []ir.IRInputFileInfo{
		{
			File:       file,
			Name:       "testmodule",
			ModulePath: "test.module/tm",
		},
	}
	irData, err := ir.Parse(
		modNames,
		modDefaultNames,
		input,
		func(modulePath string) (map[string]*ast.File, error) {
			return nil, fmt.Errorf("getDependency not implemented")
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return irData
}

func TestConstsVars(t *testing.T) {
	assert := assert.New(t)

	irData := parseSingleFile(t, "testdata/consts_vars.go")
	assert.Equal(irData.Values["testmodule.Pi"].Type.Name, "float64")
	assert.Equal(irData.Values["testmodule.Text"].Type.Name, "string")
	assert.Equal(irData.Values["testmodule.Answer"].Type.Name, "int64")
	assert.Equal(irData.Values["testmodule.SomeStuff"].Type.Name, "[3]int")
	assert.Equal(irData.Values["testmodule.EnumVal0"].Type.Name, "int64")
	assert.Equal(irData.Values["testmodule.EnumVal3"].Type.Name, "int64")
}

func TestConstexprArrays(t *testing.T) {
	assert := assert.New(t)

	irData := parseSingleFile(t, "testdata/constexpr_arrays.go")
	assert.Equal(irData.Structs["testmodule.Example"].Fields[0].Type.Name, "[77]uint8")
	assert.Equal(irData.Structs["testmodule.Example"].Fields[1].Type.Name, "[]uint8")
}

func TestDocComments(t *testing.T) {
	assert := assert.New(t)

	irData := parseSingleFile(t, "testdata/doc_comments.go")
	assert.Equal(irData.Funcs["testmodule.AddTwoInts"].DocComment, `Add two integers.
Very useful.
`)
}
