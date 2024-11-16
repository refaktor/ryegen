package testfile

type Example interface {
	MyFn(a ...string)
	Unused(x int)
}

func DoSomething(a Example) {
	_ = e
}

func Functor(f func(a ...any)) {
	_ = f
}
