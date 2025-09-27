package main

import (
	"fmt"
	my_fmt "fmt"
)

// This binding demonstrates import pruning.
// In this case, the "fmt" import is only used
// inside of a function body, so we don't need
// to generate any fmt-related bindings, or
// even type-check the "fmt" package.
func Hello() {
	fmt.Println("Hello, world!")
}

// ...should also work with named import path.
func Hello2() {
	my_fmt.Println()
}
