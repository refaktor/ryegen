package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/refaktor/ryegen/v2/bindspec"
	"github.com/refaktor/ryegen/v2/converter"
	"github.com/refaktor/ryegen/v2/preprocessor"
	"github.com/stretchr/testify/require"
)

func checkFile(t *testing.T, dir, name string) {
	t.Helper()

	require := require.New(t)

	inFileName := name + ".in.go"
	ryeProgramName := name + ".rye"
	expectedOutputPath := filepath.Join(dir, name+".expected_output")
	bindspecPath := filepath.Join(dir, name+".bindspec")
	require.FileExists(filepath.Join(dir, inFileName))
	require.FileExists(filepath.Join(dir, ryeProgramName))
	require.FileExists(expectedOutputPath)

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(dir, inFileName), nil, parser.SkipObjectResolution|parser.ParseComments)
	require.NoError(err)

	err = preprocessor.Preprocess(fset, f, func(path string) (string, error) {
		// NOTE: This just guesses the package import
		// name. Connect this with moduleset's data
		// for usage outside of tests.
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash == -1 {
			return path, nil
		} else {
			return path[lastSlash+1:], nil
		}
	})
	require.NoError(err)

	conf := &types.Config{
		Context:          types.NewContext(),
		GoVersion:        "go1.23",
		IgnoreFuncBodies: true,
		FakeImportC:      true,
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
	_, err = conf.Check("", fset, []*ast.File{f}, info)
	require.NoError(err)

	convs := map[string]string{}
	namedTyps := map[string]map[string]*types.Named{} // package and name to named type
	imports := map[string]struct{}{}
	var generateConv func(spec converter.ConverterSpec) error
	generateConv = func(spec converter.ConverterSpec) error {
		name := spec.Name()
		if _, ok := convs[name]; ok {
			return nil
		}
		if typ, ok := unpointer(spec.Type()).(*types.Named); ok && typ.Obj().Pkg() != nil {
			pkg := typ.Obj().Pkg().Path()
			if namedTyps[pkg] == nil {
				namedTyps[pkg] = map[string]*types.Named{}
			}
			namedTyps[pkg][typ.Obj().Name()] = typ
		}
		text, dependencies, err := spec.Generate()
		if err != nil {
			return err
		}
		convs[name] = text
		for _, imp := range dependencies.Imports {
			imports[imp.Path()] = struct{}{}
		}
		for _, dep := range dependencies.Converters {
			if err := generateConv(dep); err != nil {
				return err
			}
		}
		return nil
	}
	var bindingFuncs []bindingFunc
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if decl.Name.IsExported() {
				bf, spec := newBindingFunc(info.ObjectOf(decl.Name).(*types.Func))
				err := generateConv(spec)
				require.NoError(err)
				bindingFuncs = append(bindingFuncs, bf)
			}
		case *ast.GenDecl:
			if decl.Tok == token.TYPE {
				for _, spec := range decl.Specs {
					if spec, ok := spec.(*ast.TypeSpec); ok {
						if _, ok := spec.Type.(*ast.InterfaceType); ok {
							typ := info.ObjectOf(spec.Name).Type().Underlying().(*types.Interface)
							for m := range typ.Methods() {
								if !m.Exported() {
									continue
								}
								bf, spec := newBindingFunc(m)
								err := generateConv(spec)
								require.NoError(err)
								bindingFuncs = append(bindingFuncs, bf)
							}
						}
					}
				}
			}
		}
	}

	var bs *bindspec.Program
	if _, err := os.Stat(bindspecPath); err == nil {
		b, err := os.ReadFile(bindspecPath)
		require.NoError(err)
		bs, err = bindspec.Parse(bindspecPath, b)
		require.NoError(err)
	} else {
		require.ErrorIs(err, os.ErrNotExist)
	}

	var bsIface bindspec.Interface
	{
		bsIface.Pkgs = []string{""}
		bsIface.Names = map[string][]string{}
		namesSeen := map[string]bool{}
		for _, bf := range bindingFuncs {
			if !namesSeen[bf.name] {
				bsIface.Names[""] = append(bsIface.Names[""], bf.name)
				namesSeen[bf.name] = true
			}
		}
		bfIdxsByName := map[string][]int{}
		for i, bf := range bindingFuncs {
			bfIdxsByName[bf.name] = append(bfIdxsByName[bf.name], i)
		}
		getBindingFuncIdxs := func(pkg, name string) []int {
			if pkg != "" {
				panic("invalid pkg passed to getBindingFuncIdx: " + pkg)
			}
			bfIdx, ok := bfIdxsByName[name]
			if !ok {
				panic("invalid name passed to getBindingFuncIdx: " + name)
			}
			return bfIdx
		}
		bsIface.Rename = func(pkg, name, newName string) {
			bfIdxs := getBindingFuncIdxs(pkg, name)
			for _, bfIdx := range bfIdxs {
				bindingFuncs[bfIdx].name = newName
			}
			delete(bfIdxsByName, name)
			bfIdxsByName[newName] = bfIdxs
		}
		bsIface.SetIncluded = func(pkg, name string, included bool) {
			bfIdxs := getBindingFuncIdxs(pkg, name)
			for _, bfIdx := range bfIdxs {
				bindingFuncs[bfIdx].exclude = !included
			}
		}
	}
	if bs != nil {
		err = bindspec.Run(bs, bsIface)
		require.NoError(err)
	}

	convsFileName := name + ".out_convs.go"
	{
		var out bytes.Buffer
		out.WriteString("package main\n\n")
		out.WriteString(converter.PreludeCode)
		out.WriteString("\n")
		for _, k := range slices.Sorted(maps.Keys(convs)) {
			conv := convs[k]
			out.WriteString(conv)
			out.WriteString("\n\n")
		}
		err := os.WriteFile(filepath.Join(dir, convsFileName), out.Bytes(), 0666)
		require.NoError(err)
	}

	builtinsFileName := name + ".out_builtins.go"
	{
		var out bytes.Buffer
		out.WriteString("package main\n\n")
		out.WriteString(builtinsCommonCode)
		out.WriteString("var typeLookup = map[string]map[string]nativeTypeEntry{\n")
		for _, pkg := range slices.Sorted(maps.Keys(namedTyps)) {
			typs := namedTyps[pkg]
			if pkg == "" {
				pkg = "main"
			}
			fmt.Fprintf(&out, "\t"+`"%v": map[string]nativeTypeEntry{`+"\n", pkg)
			for _, name := range slices.Sorted(maps.Keys(typs)) {
				typ := typs[name]
				//_, isStruct := typ.Underlying().(*types.Struct)
				fmt.Fprintf(&out, "\t\t"+`"%v": {"%v"},`+"\n", name, types.TypeString(typ, converter.PkgImportNameQualifier))
			}
			out.WriteString("\t},\n")
		}
		out.WriteString("}\n\n")
		out.WriteString("var builtins0 = map[string]*_env.VarBuiltin{\n")
		for _, fn := range bindingFuncs {
			if fn.exclude {
				continue
			}
			bindingKey, builtin := fn.builtin(converter.PkgImportNameQualifier)
			fmt.Fprintf(&out, "\t"+`"%v": %v,`+"\n", bindingKey, builtin)
		}
		out.WriteString("}\n\n")
		out.WriteString(`var builtins = map[string]map[string]*_env.VarBuiltin{"example.com": builtins0}` + "\n\n")
		err := os.WriteFile(filepath.Join(dir, builtinsFileName), out.Bytes(), 0666)
		require.NoError(err)
	}

	cmd := exec.Command("go", "run", name+".in.go", convsFileName, builtinsFileName, ryeProgramName)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		if err, ok := err.(*exec.ExitError); ok {
			t.Fatalf("non-zero exit code; stderr: %s", err.Stderr)
		}
		require.NoError(err)
	}
	expectedOutput, err := os.ReadFile(expectedOutputPath)
	require.NoError(err)
	require.Equal(string(expectedOutput), string(output), "rye program output doesn't match")
}

func TestRyegen(t *testing.T) {
	tStart := time.Now()
	defer func() {
		fmt.Println("time:", time.Since(tStart))
	}()

	require := require.New(t)

	dir, err := os.ReadDir("testdata")
	require.NoError(err)
	for _, ent := range dir {
		if !ent.Type().IsRegular() {
			continue
		}
		if name, ok := strings.CutSuffix(ent.Name(), ".in.go"); ok {
			t.Run(name, func(t *testing.T) {
				checkFile(t, "testdata", name)
			})
		}
	}
}
