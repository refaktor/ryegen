package bindertest_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/refaktor/ryegen/binder"
	"github.com/refaktor/ryegen/config"
	"github.com/refaktor/ryegen/ir"
	"github.com/refaktor/ryegen/ir/irtest"
)

func testGen(t *testing.T, src string, genOut ...func(irData *ir.IR, deps *binder.Dependencies, ctx *binder.Context) string) {
	t.Helper()

	if !strings.HasSuffix(src, ".go") {
		panic("expected .go file as src")
	}
	cmpFile := src[:len(src)-3] + ".out.go"

	assert := assert.New(t)

	irData, modNames := irtest.ParseSingleFile(t, src)
	ctx := binder.NewContext(&config.Config{}, irData, modNames)

	deps := binder.NewDependencies()

	var outW strings.Builder
	for i, g := range genOut {
		if i != 0 {
			fmt.Fprintf(&outW, "\n//================================//\n\n")
		}
		outW.WriteString(g(irData, deps, ctx))
	}
	out := outW.String()

	if !assert.FileExists(cmpFile) {
		os.WriteFile(cmpFile, []byte(out), 0666)
		assert.Failf("No output comparison file found", "Wrote %v", cmpFile)
	}

	expect, err := os.ReadFile(cmpFile)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(out, string(expect))
}

func TestVararg(t *testing.T) {
	assert := assert.New(t)

	testGen(t, "testdata/varargs.go",
		func(irData *ir.IR, deps *binder.Dependencies, ctx *binder.Context) string {
			ifaceImpl, err := binder.GenerateGenericInterfaceImpl(deps, ctx, irData.Interfaces["testmodule.Example"])
			if err != nil {
				t.Fatal(err)
			}
			return ifaceImpl
		},
		func(irData *ir.IR, deps *binder.Dependencies, ctx *binder.Context) string {
			bf, err := binder.GenerateBinding(deps, ctx, irData.Funcs["testmodule.DoSomething"])
			if err != nil {
				t.Fatal(err)
			}
			return bf.Body
		},
		func(irData *ir.IR, deps *binder.Dependencies, ctx *binder.Context) string {
			bf, err := binder.GenerateBinding(deps, ctx, irData.Funcs["testmodule.Functor"])
			if err != nil {
				t.Fatal(err)
			}
			return bf.Body
		},
	)

	testGen(t, "testdata/arrays.go",
		func(irData *ir.IR, deps *binder.Dependencies, ctx *binder.Context) string {
			bf, err := binder.GenerateBinding(deps, ctx, irData.Funcs["testmodule.ProcessSlice"])
			if err != nil {
				t.Fatal(err)
			}
			return bf.Body
		},
		func(irData *ir.IR, deps *binder.Dependencies, ctx *binder.Context) string {
			bf, err := binder.GenerateBinding(deps, ctx, irData.Funcs["testmodule.ProcessSliceSlice"])
			if err != nil {
				t.Fatal(err)
			}
			return bf.Body
		},
	)

	{
		filename := "testdata/doccomments.go"
		irData, modNames := irtest.ParseSingleFile(t, filename)
		ctx := binder.NewContext(&config.Config{}, irData, modNames)

		deps := binder.NewDependencies()

		bf, err := binder.GenerateBinding(deps, ctx, irData.Funcs["testmodule.FuncWithDoc"])
		if err != nil {
			t.Fatal(err)
		}

		assert.Equal(bf.DocComment, `This text should be in the generated doc string.

Args:
 * x - block(len=4)[integer]
 * y - dict[integer, string]
Result:
 * string
 * error
`)
	}
}
