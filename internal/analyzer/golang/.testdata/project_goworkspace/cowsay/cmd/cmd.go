package cmd

import (
	"fmt"

	"example.com/cowsay/moo"
)

func Run() {
	fmt.Println(moo.Say("moo"))
}
