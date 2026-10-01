package main
import (
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
)
func main() {
	_, err := refdb.Open("refdata/settings.db")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Println("Success")
	}
}
