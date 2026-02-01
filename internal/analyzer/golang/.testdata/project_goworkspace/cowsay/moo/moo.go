package moo

import "fmt"

func Say(msg string) string {
	return fmt.Sprintf(" %s\n -----\n      \\   ^__^\n       \\  (oo)\\_______\n          (__)\\       )\n              ||----w |\n              ||     ||", msg)
}
