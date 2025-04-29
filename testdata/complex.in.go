// Some more complex bindings to test a variety of features
// and converters.
package main

import (
	"errors"
	"fmt"
)

const (
	_ = iota + 40
	_
	FortyTwo
)

type MyType struct {
	Text string
}

func Answer() int {
	return FortyTwo
}

// Using calculated array size
// (tests constant folding).
func Block() (vals [FortyTwo]int) {
	for i := range vals {
		vals[i] = i
	}
	return
}

func NewMyType() MyType {
	return MyType{
		Text: "Hello",
	}
}

func WhatDoesMyTypeSay(x MyType) string {
	return "Text: " + x.Text
}

func IFail() error {
	return errors.New("something failed")
}

func ToInteger(x float64) int {
	return int(x)
}

func Add(vals []int) int {
	acc := 0
	for _, x := range vals {
		acc += x
	}
	return acc
}

func Process5Ints(_ [5]int) {
}

func MakeADict() map[string]int {
	return map[string]int{
		"One":  1,
		"Ten":  10,
		"Five": 5,
	}
}

func FuncTakingIntPtr(x *int) string {
	if x == nil {
		return "<nil>"
	}
	return fmt.Sprint(*x)
}

func MultiReturn() (string, int) {
	return "hello", 123
}

func MultiReturnError(fail bool) (string, int, error) {
	var err error
	if fail {
		err = errors.New("this should fail")
	}
	return "world", 456, err
}

func ThisShouldntBeGenerated() {
}

func ReplaceMyNameWithABC() {

}

type T1 struct{}

// Should implicitly generate a T1 converter.
func FuncTakingT1Ptr(x *T1) {}

type T2 struct{}

func NewT2() *T2 {
	return &T2{}
}

func (t *T2) SayHello() {
	fmt.Println("Hello from T3!")
}

func (t *T2) Say(x string) string {
	return fmt.Sprint("T3: ", x)
}

type I interface {
	SayHello()
}

func NewT2AsI() I {
	return NewT2()
}
