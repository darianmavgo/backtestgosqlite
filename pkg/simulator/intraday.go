package simulator

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// Intraday maps a symbol to its intraday bars (hourly) in time order. Each bar's
// Date is the Eastern time "YYYY-MM-DD HH:MM" its hour starts, and only regular
// session hours are kept (see runner.LoadIntraday). A nil Intraday means the
// daily bars are all there is.
type Intraday map[string][]models.Bar

// intradaySession returns the intraday bars of daily's session. covered is false
// when the symbol has no intraday bars on either side of that date, so the daily
// bar is the only evidence there is. A date inside the covered span with no bars
// returns covered with an empty session: nothing can confirm a fill, so none is
// made.
//
// The hour that starts at 9:00 holds the half hour before the open. Its open is
// replaced by the daily open and its range is cut to the daily range, which
// is regular session only.
func intradaySession(hourly []models.Bar, daily models.Bar) (session []models.Bar, covered bool) {
	if len(hourly) == 0 {
		return nil, false
	}
	first, last := hourly[0].Date[:10], hourly[len(hourly)-1].Date[:10]
	if daily.Date < first || daily.Date > last {
		return nil, false
	}
	lo := sort.Search(len(hourly), func(i int) bool { return hourly[i].Date[:10] >= daily.Date })
	for i := lo; i < len(hourly) && hourly[i].Date[:10] == daily.Date; i++ {
		b := hourly[i]
		if b.Date[11:13] == "09" {
			b.Open = daily.Open
			b.Low = max(b.Low, daily.Low)
			b.High = min(b.High, daily.High)
		}
		session = append(session, b)
	}
	return session, true
}

// fillOnIntraday finds where a buy limit fills in a session. A daily open under
// the limit fills at that open, in the first bar, and the whole first bar is
// after the fill. Otherwise the order fills in the first bar whose low reaches
// the limit, at the limit or at that bar's open when the open is already under
// it, and only that bar's low counts after the fill (its high is cut to the fill
// price) because the order of touches inside a bar is unknown. It returns the
// bars from the fill bar on and the fill price.
func fillOnIntraday(session []models.Bar, daily models.Bar, limit float64) ([]models.Bar, float64, bool) {
	if len(session) == 0 {
		return nil, 0, false
	}
	if daily.Open > 0 && daily.Open < limit {
		return append([]models.Bar(nil), session...), daily.Open, true
	}
	for i, b := range session {
		if b.Low > limit {
			continue
		}
		fill := limit
		if b.Open > 0 && b.Open < limit {
			fill = b.Open
		}
		after := append([]models.Bar(nil), session[i:]...)
		after[0].High = fill
		return after, fill, true
	}
	return nil, 0, false
}
