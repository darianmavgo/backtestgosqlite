package appenv

import (
	"path/filepath"
	"testing"
)

func TestReportFileNeverDoublesPrefix(t *testing.T) {
	t.Setenv("APP_FOLDER", "/app")
	want := filepath.Join("/app", "data", "reports", "x.html")
	for _, in := range []string{"x.html", "reports/x.html", "data/reports/x.html"} {
		if got := ReportFile(in); got != want {
			t.Errorf("ReportFile(%q) = %q, want %q", in, got, want)
		}
	}
}
