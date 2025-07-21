package main

import "fmt"

type X struct {
	V string
}

func NewX(v string) X {
	return X{V: v}
}

func (x X) Hello() {
	fmt.Println(x.V)
}

type Y int

func NewY(v int) Y {
	return Y(v)
}

func (y Y) Hello() {
	fmt.Println(int(y))
}

type Z struct {
	V string
}

func NewZ(v string) *Z {
	return &Z{V: v}
}

func (z *Z) Hello() {
	fmt.Println(z.V)
}

type W int

func NewW(v int) *W {
	w := new(W)
	*w = W(v)
	return w
}

func (w *W) Hello() {
	fmt.Println(*w)
}

type I interface {
	Hello()
}

func IToOpaque(v I) I { return v }

func HelloX(x X) { x.Hello() }

func ToAnyOpaque(v interface{}) any { return v }

// This isn't an actual interface, so it shouldn't be treated as one
type Floating interface {
	~float32 | ~float64
}
