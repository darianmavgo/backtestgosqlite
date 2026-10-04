package main

import (
	"fmt"
	"log"
	"os"

	"github.com/darianmavgo/backtestgosqlite/pkg/walk_forward"
)

func main() {
	fmt.Fprintln(os.Stderr, "note: walk_forward is deprecated, use `validate walk` (see README: validate)")
	if err := walk_forward.Main(os.Stdout); err != nil {
		log.Fatal(err)
	}
}
