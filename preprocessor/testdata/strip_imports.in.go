package main

import (
	"bytes"
	"fmt"
	. "strings" // imports as "." shouldn't be stripped, their its necessity can't be determined without importing the package
)

func Test() {
	fmt.Println("hi")
	b := bytes.NewBuffer()
	_ = b
	x := Split("a/b/c")
	_ = x
}
