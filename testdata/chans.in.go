package main

import "fmt"

func Get1And2() <-chan int {
	ch := make(chan int)
	go func() {
		ch <- 1
		ch <- 2
		close(ch)
	}()
	return ch
}

func Printer(done chan<- struct{}) chan<- int {
	ch := make(chan int)
	go func() {
		for i := range ch {
			fmt.Println(i)
		}
		done <- struct{}{}
	}()
	return ch
}

func Doubler(done chan<- struct{}) chan int {
	ch := make(chan int)
	go func() {
		for i := range ch {
			ch <- 2 * i
		}
		done <- struct{}{}
	}()
	return ch
}

func PrintChan(ch <-chan int) {
	for i := range ch {
		fmt.Println(i)
	}
}
