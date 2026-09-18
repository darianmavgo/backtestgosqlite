package refdb

import (
	"path/filepath"
	"testing"
)

func TestUniverseAndDTRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "ref.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := SaveUniverse(db, ListSweep, []string{"voo", " spxu ", "", "VOO"}); err != nil {
		t.Fatal(err)
	}
	got, err := Universe(db, ListSweep)
	if err != nil || len(got) != 2 || got[0] != "SPXU" || got[1] != "VOO" {
		t.Fatalf("Universe = %v, %v", got, err)
	}

	rows := []DTStrategy{{Symbol: "AAA", Score: 1}, {Symbol: "BBB", Score: 2}}
	if err := SaveDTStrategies(db, rows); err != nil {
		t.Fatal(err)
	}
	dt, err := DTStrategies(db)
	if err != nil || len(dt) != 2 || dt[0].Symbol != "BBB" {
		t.Fatalf("DTStrategies = %v, %v", dt, err)
	}
}
