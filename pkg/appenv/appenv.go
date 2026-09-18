// Package appenv resolves configuration from the process environment and the
// project .env file, so every command agrees on the API key and where the
// reports, reference data and market data live.
//
//	POLYGON_API_KEY  Polygon.io key
//	APP_FOLDER       project root (default: current directory)
//	APP_REPORTS      reports folder, relative to APP_FOLDER (default: reports)
//	APP_REF          reference-data folder, relative to APP_FOLDER (default: refdata)
//	APP_DATA         market-data folder, relative to APP_FOLDER (default: data)
//
// Real environment variables win over .env values. .env is searched in the
// current directory, its two parents, and beside/above the running binary.
package appenv

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	once   sync.Once
	fileKV map[string]string
)

func load() {
	fileKV = map[string]string{}
	dirs := []string{".", "..", "../.."}
	if exe, err := os.Executable(); err == nil { // bin/<cmd> -> project root
		d := filepath.Dir(exe)
		dirs = append(dirs, d, filepath.Dir(d))
	}
	for _, dir := range dirs {
		f, err := os.Open(filepath.Join(dir, ".env"))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(strings.TrimPrefix(k, "export "))
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if _, seen := fileKV[k]; !seen { // nearest .env wins
				fileKV[k] = v
			}
		}
		f.Close()
	}
}

// Get returns the value for key from the environment, then .env ("" if unset).
func Get(key string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	once.Do(load)
	return strings.TrimSpace(fileKV[key])
}

func sub(key, def string) string {
	if v := Get(key); v != "" {
		return filepath.Clean(v)
	}
	return def
}

// Folder is the project root.
func Folder() string { return sub("APP_FOLDER", ".") }

// Reports is the reports directory.
func Reports() string { return filepath.Join(Folder(), sub("APP_REPORTS", "reports")) }

// Ref is the reference-data directory.
func Ref() string { return filepath.Join(Folder(), sub("APP_REF", "refdata")) }

// Data is the market-data directory.
func Data() string { return filepath.Join(Folder(), sub("APP_DATA", "data")) }

// RefDB is the reference database (universes, DT configs, symbol tables).
func RefDB() string { return filepath.Join(Ref(), "settings.db") }

// MarketDB is the market history database.
func MarketDB() string { return filepath.Join(Data(), "market_history.db") }

// DataFile returns a path to name inside the data directory.
func DataFile(name string) string { return filepath.Join(Data(), name) }

// ReportFile returns a path inside the reports directory. Absolute paths pass
// through; a leading "reports/" is treated as the reports directory itself.
func ReportFile(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	p = strings.TrimPrefix(filepath.Clean(p), "reports"+string(filepath.Separator))
	return filepath.Join(Reports(), p)
}
