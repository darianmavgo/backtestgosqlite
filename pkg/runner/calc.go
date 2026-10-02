package runner

import (
	"fmt"
	"os"
	"path/filepath"
)

// KeepCalc keeps the per-strategy SQL calculation databases (the slice tables
// a pipeline builds) under <out-dir>/calc/ so any stage can be queried. Off by
// default: they are scratch files, thousands of them in a bulk run, and are
// deleted when the run finishes. Set by backtest -keep-calc.
var KeepCalc bool

// calcDBPath returns the calc database path for one strategy run and a cleanup
// function to call when the run is done.
func calcDBPath(outDir, id string) (string, func()) {
	dir, cleanup := calcDir(outDir)
	return filepath.Join(dir, fmt.Sprintf("calc_%s.db", id)), cleanup
}

// calcDir returns a directory for calc databases and its cleanup function. With
// KeepCalc it is <outDir>/calc and cleanup does nothing; otherwise it is a
// fresh temporary directory removed (WAL and SHM files included) by cleanup.
func calcDir(outDir string) (string, func()) {
	if KeepCalc {
		dir := filepath.Join(outDir, "calc")
		_ = os.MkdirAll(dir, 0o755)
		return dir, func() {}
	}
	dir, err := os.MkdirTemp("", "bt-calc-")
	if err != nil {
		// Fall back to the reports folder rather than failing the run.
		dir = filepath.Join(outDir, "calc")
		_ = os.MkdirAll(dir, 0o755)
		return dir, func() {}
	}
	return dir, func() { _ = os.RemoveAll(dir) }
}
