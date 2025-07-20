package main

func Test(x int, y float32) int {
	return x + int(y)
}

func TestGen[T any](x T) *T {
	return &x
}
