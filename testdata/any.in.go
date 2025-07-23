package main

import "fmt"

func TypeAndValue(a any) {
	fmt.Printf("%T %v\n", a, a)
}

type SomeStruct struct {
	A int
	B int
	C int
}
