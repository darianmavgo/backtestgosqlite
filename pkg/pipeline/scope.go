package pipeline

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	sqlfiles "github.com/darianmavgo/backtestgosqlite/sql"
)

var safeSymbol = regexp.MustCompile(`^[A-Z0-9.^=_-]+$`)

// cleanSymbols upper-cases, trims and de-duplicates symbols, rejecting any that
// could not be a ticker. The list goes into SQL, so nothing else gets through.
func cleanSymbols(in []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		if !safeSymbol.MatchString(s) {
			return nil, fmt.Errorf("pipeline: %q is not a ticker symbol", s)
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// strategiesFor returns the ids of every strategy row tied to the symbols in the
// reference database at refDB, by sql/stages/pipeline_scope/01_by_symbol.sql.
func strategiesFor(refDB string, symbols []string) ([]string, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("pipeline: no symbols")
	}
	text, err := sqlfiles.Stages.ReadFile("stages/pipeline_scope/01_by_symbol.sql")
	if err != nil {
		return nil, err
	}
	quoted := make([]string, len(symbols))
	for i, s := range symbols {
		quoted[i] = "'" + s + "'"
	}
	q := strings.ReplaceAll(string(text), "__SYMBOL_LIST__", strings.Join(quoted, ","))
	db, err := storage.OpenSQLiteReadOnly(refDB)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var ids []string
	if err := db.Select(&ids, q); err != nil {
		return nil, fmt.Errorf("pipeline: strategies for %v: %w", symbols, err)
	}
	return ids, nil
}

// checkRegistered returns an error naming every id that no registry resolves.
func checkRegistered(ids []string) error {
	var missing []string
	for _, id := range ids {
		if _, ok := strategy.Get(id); !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("pipeline: not registered: %s", strings.Join(missing, ", "))
	}
	return nil
}

// cutoffFor is the date 12 months before the last daily bar of the symbols, where
// a parameter sweep stops so the held-out months do not tune it. It reads
// sql/stages/pipeline_scope/02_cutoff.sql.
func cutoffFor(marketDB string, symbols []string) (string, error) {
	text, err := sqlfiles.Stages.ReadFile("stages/pipeline_scope/02_cutoff.sql")
	if err != nil {
		return "", err
	}
	quoted := make([]string, len(symbols))
	for i, s := range symbols {
		quoted[i] = "'" + s + "'"
	}
	q := strings.ReplaceAll(string(text), "__SYMBOL_LIST__", strings.Join(quoted, ","))
	db, err := storage.OpenSQLiteReadOnly(marketDB)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var cutoff *string
	if err := db.Get(&cutoff, q); err != nil {
		return "", err
	}
	if cutoff == nil || *cutoff == "" {
		return "", fmt.Errorf("pipeline: no daily bars for %v in %s", symbols, marketDB)
	}
	return *cutoff, nil
}
