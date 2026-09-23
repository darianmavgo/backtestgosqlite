package calendar

import (
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestIsHoliday(t *testing.T) {
	yes := []time.Time{
		day(2025, 1, 1), day(2025, 1, 20), day(2025, 2, 17), day(2025, 4, 18), // NY, MLK, Presidents, Good Friday
		day(2025, 5, 26), day(2025, 6, 19), day(2025, 7, 4), day(2025, 9, 1),
		day(2025, 11, 27), day(2025, 12, 25),
		day(2026, 4, 3),   // Good Friday 2026
		day(2027, 12, 24), // Christmas Saturday -> observed Friday
		day(2026, 7, 3),   // Jul 4 Saturday -> observed Friday
		day(2022, 12, 26), // Christmas Sunday -> Monday
		day(2023, 1, 2),   // Jan 1 Sunday -> Monday
	}
	for _, d := range yes {
		if !IsHoliday(d) {
			t.Errorf("%s should be a holiday", d.Format("2006-01-02"))
		}
	}
	no := []time.Time{
		day(2022, 12, 30), // Jan 1 2022 was Saturday: NOT observed the Friday before
		day(2021, 6, 18),  // Juneteenth not yet a holiday
		day(2025, 11, 28), // early close, still a session
		day(2025, 12, 24),
	}
	for _, d := range no {
		if IsHoliday(d) {
			t.Errorf("%s should not be a holiday", d.Format("2006-01-02"))
		}
	}
}

func TestTradingDaysBetween(t *testing.T) {
	if got := TradingDaysBetween(day(2025, 1, 3), day(2025, 1, 6)); got != 1 { // Fri -> Mon
		t.Errorf("Fri->Mon = %d, want 1", got)
	}
	// Thu Nov 20 -> Mon Dec 1 2025: Fri 21, Mon 24, Tue 25, Wed 26, (Thu 27 closed), Fri 28, Mon Dec 1 = 6
	if got := TradingDaysBetween(day(2025, 11, 20), day(2025, 12, 1)); got != 6 {
		t.Errorf("across Thanksgiving = %d, want 6", got)
	}
	if got := TradingDaysBetween(day(2025, 4, 16), day(2025, 4, 21)); got != 2 { // Good Friday closed: Thu 17, Mon 21
		t.Errorf("across Good Friday = %d, want 2", got)
	}
	if got := TradingDaysBetween(day(2025, 1, 6), day(2025, 1, 3)); got != 0 {
		t.Errorf("reversed = %d, want 0", got)
	}
}
