package main

import (
	"fmt"

	"example.com/foobarbaz/internal/greet"
)

func main() {
	fmt.Println(greet.Hello("world"))
}
