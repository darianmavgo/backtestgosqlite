package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
)

func main() {
	if len(os.Args) > 2 {
		fmt.Println("Usage: ./bin/dataflare [path_to_db]")
		os.Exit(1)
	}

	args := []string{"-a", "Dataflare"}

	if len(os.Args) == 2 {
		dbPath := os.Args[1]
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			log.Fatalf("Error: Database file '%s' does not exist.", dbPath)
		}
		args = append(args, dbPath)
		fmt.Printf("🚀 Launching Dataflare for database: %s\n", dbPath)
	} else {
		fmt.Println("🚀 Launching Dataflare...")
	}

	cmd := exec.Command("open", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to launch Dataflare: %v\nMake sure Dataflare is installed in /Applications.", err)
	}
}
