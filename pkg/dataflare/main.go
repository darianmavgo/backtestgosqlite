package dataflare

import (
	"fmt"
	"log"
	"os"
	"os/exec"
)

// Config holds the settings of a Dataflare launch.
type Config struct {
	DB string // optional database to open; empty launches the app only
}

// Main is the CLI entry point.
func Main() {
	if len(os.Args) > 2 {
		fmt.Println("Usage: ./bin/dataflare [path_to_db]")
		os.Exit(1)
	}
	var conf Config
	if len(os.Args) == 2 {
		conf.DB = os.Args[1]
	}
	if err := Run(conf); err != nil {
		log.Fatal(err)
	}
}

// Run launches the Dataflare macOS app, optionally opening conf.DB.
func Run(conf Config) error {
	args := []string{"-a", "Dataflare"}

	if conf.DB != "" {
		if _, err := os.Stat(conf.DB); os.IsNotExist(err) {
			return fmt.Errorf("database file '%s' does not exist", conf.DB)
		}
		args = append(args, conf.DB)
		fmt.Printf("🚀 Launching Dataflare for database: %s\n", conf.DB)
	} else {
		fmt.Println("🚀 Launching Dataflare...")
	}

	cmd := exec.Command("open", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to launch Dataflare: %w\nMake sure Dataflare is installed in /Applications", err)
	}
	return nil
}
