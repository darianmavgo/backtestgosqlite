package refdb

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

var safeStrategyID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Export writes a small reference database at dstPath holding only the strategy
// rows named in ids, copied from the full database at srcPath. It is how a program
// that cannot carry the whole file (trade_orchestrator on App Engine) ships the
// handful of rows it trades. An id found in none of the four row tables is an error,
// so a typo cannot ship as an empty bundle. It returns the ids found, by table.
func Export(srcPath, dstPath string, ids []string) (map[string][]string, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("export: no strategy ids")
	}
	quoted := make([]string, len(ids))
	for i, id := range ids {
		if !safeStrategyID.MatchString(id) {
			return nil, fmt.Errorf("export: %q is not a strategy id", id)
		}
		quoted[i] = "'" + id + "'"
	}
	if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	db, err := storage.OpenSQLite(dstPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // the ATTACH must be seen by the statements that follow
	if _, err := db.Exec(`ATTACH DATABASE ? AS src`, srcPath); err != nil {
		return nil, fmt.Errorf("export: attach %s: %w", srcPath, err)
	}
	if err := storage.RunStage(db, "refdb_export", map[string]string{"__ID_LIST__": strings.Join(quoted, ",")}); err != nil {
		return nil, err
	}
	found := map[string][]string{}
	have := map[string]bool{}
	for _, table := range []string{"streak_strategy", "tree_strategy", "hold_strategy", "markov_strategy"} {
		var got []string
		if err := db.Select(&got, `SELECT id FROM `+table+` ORDER BY id`); err != nil {
			return nil, err
		}
		found[table] = got
		for _, id := range got {
			have[id] = true
		}
	}
	var missing []string
	for _, id := range ids {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return found, fmt.Errorf("export: not a row in any strategy table of %s: %s", srcPath, strings.Join(missing, ", "))
	}
	return found, nil
}
