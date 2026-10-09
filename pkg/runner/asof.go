package runner

import (
	"fmt"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/calendar"
)

// LastCompletedEquitySession is the most recent session date at or before now
// in America/New_York. Before the regular close (16:00 ET), the prior day is
// treated as last completed. Weekends and exchange holidays walk backward.
func LastCompletedEquitySession(now time.Time) time.Time {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.FixedZone("ET", -5*3600)
	}
	et := now.In(loc)
	if et.Hour() < 16 {
		et = et.AddDate(0, 0, -1)
	}
	day := time.Date(et.Year(), et.Month(), et.Day(), 0, 0, 0, 0, time.UTC)
	for !calendar.IsSession(day) {
		day = day.AddDate(0, 0, -1)
	}
	return day
}

// NextEquitySession returns the next session after last completed (the
// session live entries are aimed at after the tip close), skipping weekends and
// exchange holidays.
func NextEquitySession(lastCompleted time.Time) time.Time {
	d := lastCompleted.UTC().AddDate(0, 0, 1)
	d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	for !calendar.IsSession(d) {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

// ResolveLiveAsOf picks the bar date livescan should evaluate. Tip must equal
// last completed session — never run on stale. Signals on that tip are
// decisions for the next equity session.
func ResolveLiveAsOf(tipYYYYMMDD string, now time.Time) (asOf string, nextSession string, err error) {
	tip := strings.TrimSpace(tipYYYYMMDD)
	if len(tip) >= 10 {
		tip = tip[:10]
	}
	if tip == "" {
		return "", "", fmt.Errorf("empty market DB tip date")
	}
	expected := LastCompletedEquitySession(now).Format("2006-01-02")
	next := NextEquitySession(LastCompletedEquitySession(now)).Format("2006-01-02")
	if tip < expected {
		return "", "", fmt.Errorf("STALE_MARKET_DATA: market DB tip as-of %s but last completed session is %s (entries would target %s). Refresh failed to bring history current — fix the download source and retry", tip, expected, next)
	}
	return tip, next, nil
}
