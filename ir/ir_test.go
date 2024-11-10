package ir_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

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
	irData := parseSingleFile(t, "testdata/consts_vars.go")
	if irData.Values["testmodule.Pi"].Type.Name != "float64" {
		t.Fatal("expected const/var Pi to be of type float64")
	}
	if irData.Values["testmodule.Text"].Type.Name != "string" {
		t.Fatal("expected const/var Text to be of type string")
	}
	if irData.Values["testmodule.Answer"].Type.Name != "int64" {
		t.Fatal("expected const/var Answer to be of type int64")
	}
	if irData.Values["testmodule.SomeStuff"].Type.Name != "[3]int" {
		t.Fatal("expected const/var SomeStuff to be of type [3]int")
	}
	if irData.Values["testmodule.EnumVal0"].Type.Name != "int64" {
		t.Fatal("expected const/var EnumVal0 to be of type int64")
	}
	if irData.Values["testmodule.EnumVal3"].Type.Name != "int64" {
		t.Fatal("expected const/var EnumVal0 to be of type int64")
	}
}

func TestConstexprArrays(t *testing.T) {
	irData := parseSingleFile(t, "testdata/constexpr_arrays.go")
	if irData.Structs["testmodule.Example"].Fields[0].Type.Name != "[77]uint8" {
		t.Fatal("expected struct Example field 0 to be of type [77]uint8")
	}
	if irData.Structs["testmodule.Example"].Fields[1].Type.Name != "[]uint8" {
		t.Fatal("expected struct Example field 1 to be of type []uint8")
	}
}

func TestDocComments(t *testing.T) {
	irData := parseSingleFile(t, "testdata/doc_comments.go")
	expectComment := `Add two integers.
Very useful.
`
	if irData.Funcs["testmodule.AddTwoInts"].DocComment != expectComment {
		t.Fatalf("expected func AddTwoInts to have comment \"%v\"", expectComment)
	}
}
