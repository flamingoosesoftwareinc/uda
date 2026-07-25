package bar

import (
	"fmt"

	"example.com/project_gomod/internal/bar/baz"
)

func DoBar() {
	fmt.Println("doing bar")
	baz.DoBaz()
}
