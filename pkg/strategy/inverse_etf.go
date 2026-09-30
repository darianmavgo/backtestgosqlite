package strategy

import "strings"

// inverseETF maps a ticker to the opposite-direction ETF with the same
// leverage and the same underlying. Pairs are the listed bull/bear twins
// (ProShares or Direxion) that this database actually holds. A 3x bull is
// never paired with a 1x or 2x bear: that would be a different trade, not
// the inverse of the original one.
//
// Single-name leveraged ETFs (TSLL, NVDL, ...) are omitted. Their inverses
// are not in the bar history, and borrowing the bull to short it is the
// other leg of this experiment.
var inverseETF = map[string]string{}

func init() {
	pairs := [][2]string{
		// Nasdaq-100
		{"TQQQ", "SQQQ"}, // 3x
		{"QLD", "QID"},   // 2x
		{"QQQ", "PSQ"},   // 1x
		// S&P 500
		{"UPRO", "SPXU"}, // 3x ProShares
		{"SPXL", "SPXS"}, // 3x Direxion
		{"SSO", "SDS"},   // 2x
		{"SPY", "SH"},    // 1x
		// Dow
		{"UDOW", "SDOW"}, // 3x
		{"DDM", "DXD"},   // 2x
		{"DIA", "DOG"},   // 1x
		// Russell 2000
		{"TNA", "TZA"},   // 3x Direxion
		{"URTY", "SRTY"}, // 3x ProShares
		{"UWM", "TWM"},   // 2x
		{"IWM", "RWM"},   // 1x
		// Mid-cap
		{"UMDD", "SMDD"}, // 3x
		// Sector 3x
		{"TECL", "TECS"}, // technology
		{"SOXL", "SOXS"}, // semiconductors
		{"FAS", "FAZ"},   // financials
		{"LABU", "LABD"}, // biotech
		{"CURE", "RXD"},  // healthcare
		{"DRN", "DRV"},   // real estate
		{"WEBL", "WEBS"}, // internet
		{"HIBL", "HIBS"}, // high beta
		// Sector 2x
		{"ROM", "REW"}, // technology
		{"USD", "SSG"}, // semiconductors
		{"DIG", "DUG"}, // oil & gas
		{"UYM", "SMN"}, // basic materials
		{"ERX", "ERY"}, // energy
		{"GUSH", "DRIP"},
		{"NUGT", "DUST"}, // gold miners
		{"JNUG", "JDST"}, // junior gold miners
		// Country / region
		{"YINN", "YANG"}, // China 3x
		{"EDC", "EDZ"},   // emerging markets 3x
		{"BRZU", "BZQ"},  // Brazil 2x
		// Treasuries
		{"TMF", "TMV"}, // 20+ year 3x
		{"UBT", "TBT"}, // 20+ year 2x
		{"TLT", "TBF"}, // 20+ year 1x
	}
	for _, p := range pairs {
		inverseETF[p[0]] = p[1]
		inverseETF[p[1]] = p[0]
	}
}

// InverseETF returns the matched-leverage opposite ETF, if we have one.
func InverseETF(symbol string) (string, bool) {
	s, ok := inverseETF[strings.ToUpper(strings.TrimSpace(symbol))]
	return s, ok
}
