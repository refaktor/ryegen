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

func Printer() chan<- int {
	ch := make(chan int)
	go func() {
		for i := range ch {
			fmt.Println(i)
		}
	}()
	return ch
}

func Doubler() chan int {
	ch := make(chan int)
	go func() {
		for i := range ch {
			ch <- 2 * i
		}
	}()
	return ch
}

func PrintChan(ch <-chan int) {
	for i := range ch {
		fmt.Println(i)
	}
}
