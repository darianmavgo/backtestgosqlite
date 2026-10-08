package runview

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

func newRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "7")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenSQLite(filepath.Join(dir, "result.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE scores (id TEXT, cagr REAL);
		INSERT INTO scores VALUES ('a', 0.1), ('b', 0.5), ('c', -0.2)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	return root
}

func get(t *testing.T, h http.Handler, method, target string, form url.Values) (int, map[string]any) {
	t.Helper()
	var req *http.Request
	if method == http.MethodPost {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestTableSortFilterAndLimits(t *testing.T) {
	s := &server{root: newRoot(t)}
	h := s.routes()

	_, out := get(t, h, "GET", "/api/table?run=7&file=result.db&table=scores&sort=cagr&dir=desc", nil)
	rows := out["rows"].([]any)
	if len(rows) != 3 || rows[0].([]any)[0] != "b" {
		t.Fatalf("sort desc: %v", rows)
	}
	_, out = get(t, h, "GET", "/api/table?run=7&file=result.db&table=scores&f.id=c", nil)
	if out["total"].(float64) != 1 {
		t.Fatalf("filter: %v", out)
	}
	_, out = get(t, h, "GET", "/api/table?run=7&file=result.db&table=scores&sort=cagr%3Bdrop", nil)
	if len(out["rows"].([]any)) != 3 { // an unknown sort column is ignored, not interpolated
		t.Fatalf("bad sort: %v", out)
	}
}

func TestReadOnlyAndPathGuards(t *testing.T) {
	root := newRoot(t)
	s := &server{root: root}
	h := s.routes()

	code, _ := get(t, h, "POST", "/api/query", url.Values{"run": {"7"}, "file": {"result.db"}, "sql": {"DELETE FROM scores"}})
	if code == http.StatusOK {
		t.Fatal("DELETE must be refused")
	}
	code, _ = get(t, h, "POST", "/api/query", url.Values{"run": {"7"}, "file": {"result.db"}, "sql": {"WITH x AS (SELECT 1) DELETE FROM scores"}})
	if code == http.StatusOK {
		t.Fatal("a WITH ... DELETE must fail on the read-only connection")
	}
	_, out := get(t, h, "POST", "/api/query", url.Values{"run": {"7"}, "file": {"result.db"}, "sql": {"SELECT count(*) AS n FROM scores"}})
	if out["rows"].([]any)[0].([]any)[0].(float64) != 3 {
		t.Fatalf("rows were changed or unreadable: %v", out)
	}
	for _, f := range []string{"../result.db", "../../etc/passwd", "result.db/../../x.db"} {
		if code, _ := get(t, h, "GET", "/api/db?run=7&file="+url.QueryEscape(f), nil); code == http.StatusOK {
			t.Fatalf("path %q escaped the run folder", f)
		}
	}
	if code, _ := get(t, h, "GET", "/api/run?run=..", nil); code == http.StatusOK {
		t.Fatal("run id must be digits")
	}
}

func TestRunsListsNewestFirst(t *testing.T) {
	root := newRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "12"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &server{root: root}
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest("GET", "/api/runs", nil))
	var runs []runInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil || len(runs) != 2 || runs[0].ID != 12 {
		t.Fatalf("got %v %v", runs, err)
	}
}

func TestStrategyGathersDefinitionAndResults(t *testing.T) {
	root := newRoot(t)
	ref := filepath.Join(t.TempDir(), "strategies.db")
	rdb, err := storage.OpenSQLite(ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rdb.Exec(`CREATE TABLE streak_strategy (id TEXT, signal_days INTEGER, hold_days INTEGER);
		INSERT INTO streak_strategy VALUES ('streak-x', 3, 5)`); err != nil {
		t.Fatal(err)
	}
	rdb.Close()
	gdb, err := storage.OpenSQLite(filepath.Join(root, "7", "gridsearch.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gdb.Exec(`CREATE TABLE gridsearch_results (strategy_id TEXT, is_baseline INTEGER, calmar_ratio REAL, hold_days INTEGER);
		CREATE TABLE trades (strategy_id TEXT, pnl REAL);
		INSERT INTO gridsearch_results VALUES ('streak-x', 0, 9, 2), ('streak-x', 1, 1, 5), ('other', 0, 99, 1);
		INSERT INTO trades VALUES ('streak-x', 1.5)`); err != nil {
		t.Fatal(err)
	}
	gdb.Close()

	h := (&server{root: root, refDB: ref}).routes()
	_, out := get(t, h, "GET", "/api/strategy?run=7&id=streak-x", nil)
	def := out["definition"].([]any)
	if len(def) != 1 || def[0].(map[string]any)["table"] != "streak_strategy" {
		t.Fatalf("definition: %v", out["definition"])
	}
	res := out["results"].([]any)
	grid := res[0].(map[string]any)
	if grid["table"] != "gridsearch_results" || grid["total"].(float64) != 2 {
		t.Fatalf("grid: %v", grid)
	}
	if first := grid["rows"].([]any)[0].([]any); first[1].(float64) != 1 { // baseline first
		t.Fatalf("baseline not first: %v", first)
	}
	last := res[len(res)-1].(map[string]any)
	if last["table"] != "trades" || last["heavy"] != true {
		t.Fatalf("trades should be offered, not loaded: %v", last)
	}
	if code, _ := get(t, h, "GET", "/api/strategy?run=7&id="+url.QueryEscape("x' OR 1=1"), nil); code == http.StatusOK {
		t.Fatal("odd ids must be refused")
	}
}
