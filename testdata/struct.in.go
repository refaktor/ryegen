package main

import "fmt"

type X struct {
	A int
	B string
}

func PrintX(x X) {
	fmt.Printf("A=%v, B=%v\n", x.A, x.B)
}

type Y struct {
	X
}

func PrintAny(x any) {
	fmt.Printf("%+v\n", x)
}

type private struct{}

// This type isn't constructible
// from a RyeCtx, but you should
// still be able to pass it around
// opaquely.
type Unconvertible struct {
	private
}

func NewUnconvertible() Unconvertible {
	return Unconvertible{}
}

func TestUnconvertible(x Unconvertible) {
	fmt.Println("Unconvertible OK")
}

type Recursive struct {
	// We want to make sure this doesn't loop infinitely
	Next *Recursive
}

type WithTags struct {
	// Tags should be properly string-escaped in error messages etc.
	A int `json:"my-a"`
	B int `json:"my-b"`
}
