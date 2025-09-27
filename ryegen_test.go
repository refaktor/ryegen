package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/refaktor/ryegen/v2/config"
	"github.com/refaktor/ryegen/v2/converter"
	"github.com/refaktor/ryegen/v2/converter/typeset"
	"github.com/refaktor/ryegen/v2/preprocessor"
	"github.com/stretchr/testify/require"
)

func checkFile(t *testing.T, dir, name string) {
	t.Helper()

	require := require.New(t)

	inFileName := name + ".in.go"
	ryeProgramName := name + ".rye"
	expectedOutputPath := filepath.Join(dir, name+".expected_output")
	configPath := filepath.Join(dir, name+".toml")
	expectedErrorsPath := filepath.Join(dir, name+".expected_errors")
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
	_, err = conf.Check("main", fset, []*ast.File{f}, info)
	require.NoError(err)

	basePkg := "main"
	qualifier := types.Qualifier(func(p *types.Package) string {
		path := p.Path()
		if path == basePkg {
			return ""
		}
		return packagePathToImportName(path)
	})
	tset := typeset.New(qualifier)
	cs := converter.NewConverterSet(tset, basePkg)

	bindings := makePkgBindings(tset, info, []*ast.File{f})

	var cfg *config.Config
	if _, err := os.Stat(configPath); err == nil {
		cfg, err = config.Load(configPath)
		if cfgErr := (&config.Error{}); errors.As(err, &cfgErr) {
			require.NoErrorf(err, "%v", cfgErr.String())
		} else {
			require.NoError(err)
		}
	} else {
		require.ErrorIs(err, os.ErrNotExist)
	}

	if cfg != nil {
		bset := newBindingSet()
		newBindings, err := bset.addWithRules(cfg, bindings)
		require.NoError(err)
		bindings = newBindings
	}

	var expectedErrors string
	if _, err := os.Stat(expectedErrorsPath); err == nil {
		b, err := os.ReadFile(expectedErrorsPath)
		require.NoError(err)
		expectedErrors = string(b)
	} else {
		require.ErrorIs(err, os.ErrNotExist)
	}

	bindingConvNames := make([]string, len(bindings)) // same index as bindings
	for i, fn := range bindings {
		convName := cs.Add(fn.requiredConverter, converter.ToRye, fn.key())
		bindingConvNames[i] = convName
	}

	convsFileName := name + ".out_convs.go"
	var graph *converter.Graph
	var convErr *converter.ConverterError
	{
		var code []byte
		var err error
		code, graph, err = cs.Code()
		if err != nil {
			if ce, ok := err.(*converter.ConverterError); ok {
				convErr = ce
			} else {
				require.NoError(err)
			}
		}

		var out bytes.Buffer
		out.WriteString("package main\n\n")
		out.Write(code)
		err = os.WriteFile(filepath.Join(dir, convsFileName), out.Bytes(), 0666)
		require.NoError(err)
	}

	defer handleEnvConvGraph(&Logger{Writer: os.Stdout}, graph)()

	if (convErr != nil) != (expectedErrors != "") {
		expect := strings.TrimSpace(expectedErrors)
		var got string
		if convErr != nil {
			got = strings.TrimSpace(convErr.String())
		}
		require.Equal(expect, got, "expected errors must match actual errors (specify errors in <name>.expected_errors)")
	}

	builtinsFileName := name + ".out_builtins.go"
	{
		var out bytes.Buffer
		out.WriteString("package main\n\n")
		out.WriteString(builtinsCommonCode)
		out.WriteString("var builtins0 = map[string]*_env.VarBuiltin{\n")
		for i, fn := range bindings {
			if !graph.Contains(fn.requiredConverter, converter.ToRye) {
				//fmt.Println("skipped builtin", fmt.Sprintf("\t"+`"%v": %v,`, fn.key(), fn.binding(bindingConvNames[i])))
				continue
			}
			require.NoError(err)
			fmt.Fprintf(&out, "\t"+`"%v": %v,`+"\n", fn.key(), fn.binding(bindingConvNames[i]))
		}
		out.WriteString("}\n\n")
		out.WriteString(`var builtins = map[string]map[string]*_env.VarBuiltin{"example.com": builtins0}` + "\n\n")
		err := os.WriteFile(filepath.Join(dir, builtinsFileName), out.Bytes(), 0666)
		require.NoError(err)
	}

	cmd := exec.Command("go", "run", name+".in.go", builtinsFileName, convsFileName, ryeProgramName)
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
