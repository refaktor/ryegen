package main

import "fmt"

func Printer(value int) func() {
	return func() {
		fmt.Println(value)
	}
}

func RunUpToAndPrint(f func(i int) string, nMax int) {
	for i := range nMax {
		fmt.Println(f(i))
	}
}
