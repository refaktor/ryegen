package testfile

func MakeChan() chan int {
	return make(chan int)
}

func UseChan(ch chan int) {
	_ = ch
}
