package main

import (
	"log"
	"os"

	"github.com/darianmavgo/backtestgosqlite/pkg/walk_forward"
)

func main() {
	if err := walk_forward.Main(os.Stdout); err != nil {
		log.Fatal(err)
	}
}
