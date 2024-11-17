package irtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/refaktor/ryegen/ir/irtest"
)

func TestConstsVars(t *testing.T) {
	assert := assert.New(t)

	irData, _ := irtest.ParseSingleFile(t, "testdata/consts_vars.go")
	assert.Equal(irData.Values["testmodule.Pi"].Type.Name, "float64")
	assert.Equal(irData.Values["testmodule.Text"].Type.Name, "string")
	assert.Equal(irData.Values["testmodule.Answer"].Type.Name, "int64")
	assert.Equal(irData.Values["testmodule.SomeStuff"].Type.Name, "[3]int")
	assert.Equal(irData.Values["testmodule.EnumVal0"].Type.Name, "int64")
	assert.Equal(irData.Values["testmodule.EnumVal3"].Type.Name, "int64")
}

func TestConstexprArrays(t *testing.T) {
	assert := assert.New(t)

	irData, _ := irtest.ParseSingleFile(t, "testdata/constexpr_arrays.go")
	assert.Equal(irData.Structs["testmodule.Example"].Fields[0].Type.Name, "[77]uint8")
	assert.Equal(irData.Structs["testmodule.Example"].Fields[1].Type.Name, "[]uint8")
}

func TestDocComments(t *testing.T) {
	assert := assert.New(t)

	irData, _ := irtest.ParseSingleFile(t, "testdata/doc_comments.go")
	assert.Equal(irData.Funcs["testmodule.AddTwoInts"].DocComment, `Add two integers.
Very useful.
`)
}
