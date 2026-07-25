package adapter

import (
	"fmt"

	"example.com/fixture/domain"
)

func Print(e domain.Entity) { fmt.Println(e.ID) }
