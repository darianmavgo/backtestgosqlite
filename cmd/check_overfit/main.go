package main

import (
	"log"
	"os"

	"github.com/darianmavgo/backtestgosqlite/pkg/check_overfit"
)

func main() {
	if err := check_overfit.Main(os.Stdout); err != nil {
		log.Fatal(err)
	}
}
