package irtest

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

func ParseSingleFile(t *testing.T, path string) (*ir.IR, ir.UniqueModuleNames) {
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

	return irData, modNames
}
