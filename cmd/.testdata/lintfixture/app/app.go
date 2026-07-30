package app

import (
	"example.com/fixture/adapter"
	"example.com/fixture/domain"
)

func Run() { adapter.Print(domain.New("run")) }
