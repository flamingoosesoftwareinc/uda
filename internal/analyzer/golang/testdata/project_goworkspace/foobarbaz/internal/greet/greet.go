package greet

import "fmt"

func Hello(name string) string {
	return fmt.Sprintf("hello, %s!", name)
}

func Goodbye(name string) string {
	return fmt.Sprintf("goodbye, %s!", name)
}
