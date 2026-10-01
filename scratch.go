package main
import (
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/streak_strategy"
)
func main() {
	streak_strategy.RegisterFrom("refdata/settings.db")
	fmt.Println("Done")
}
