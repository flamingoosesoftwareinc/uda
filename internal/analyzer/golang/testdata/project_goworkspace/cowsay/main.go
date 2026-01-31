package main

import (
	"fmt"
	"os"

	"example.com/cowsay/cmd"
	"example.com/cowsay/moo"
)

func main() {
	msg := "moo"
	if len(os.Args) > 1 {
		msg = os.Args[1]
	}
	fmt.Println(moo.Say(msg))
	cmd.Run()
}
