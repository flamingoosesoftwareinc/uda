package cmd

import (
	"fmt"

	"example.com/project_gomod/internal/foo"
	"example.com/project_gomod/internal/bar"
)

func RunBlah() {
	fmt.Println("running blah")
	foo.DoFoo()
	bar.DoBar()
}
