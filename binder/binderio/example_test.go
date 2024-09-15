package binderio_test

import (
	"fmt"

	"github.com/refaktor/ryegen/binder/binderio"
)

func ExampleCodeBuilder() {
	var cb binderio.CodeBuilder
	cb.Linef(`package main`)
	cb.Linef(``)
	cb.Linef(`import "fmt"`)
	cb.Linef(``)
	cb.Linef(`type FmtTest struct {`)
	cb.Indent++
	cb.Linef(`A int`)
	cb.Linef(`BVeryLongName int`)
	cb.Linef(`CEvenMuchLongerNameThanB int`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(``)
	cb.Linef(`func main() {`)
	cb.Indent++
	for i := 0; i < 10; i++ {
		cb.Linef(`fmt.Println("Hello %v")`, i)
	}
	cb.Indent--
	cb.Linef(`}`)

	code, err := cb.FmtString()
	if err != nil {
		panic(err)
	}
	fmt.Println(code)
	// Output:
	// package main
	//
	// import "fmt"
	//
	// type FmtTest struct {
	// 	A                        int
	// 	BVeryLongName            int
	// 	CEvenMuchLongerNameThanB int
	// }
	//
	// func main() {
	// 	fmt.Println("Hello 0")
	// 	fmt.Println("Hello 1")
	// 	fmt.Println("Hello 2")
	// 	fmt.Println("Hello 3")
	// 	fmt.Println("Hello 4")
	// 	fmt.Println("Hello 5")
	// 	fmt.Println("Hello 6")
	// 	fmt.Println("Hello 7")
	// 	fmt.Println("Hello 8")
	// 	fmt.Println("Hello 9")
	// }
}
