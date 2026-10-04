package main

import (
	"fmt"
	"log"
	"os"

	"github.com/darianmavgo/backtestgosqlite/pkg/check_overfit"
)

func main() {
	fmt.Fprintln(os.Stderr, "note: check_overfit is deprecated, use `validate verdict` (see README: validate)")
	if err := check_overfit.Main(os.Stdout); err != nil {
		log.Fatal(err)
	}
}
