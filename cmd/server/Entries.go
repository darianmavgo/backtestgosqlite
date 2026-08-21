package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v2/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v2/marketdata"
	"github.com/davecgh/go-spew/spew"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
)

// profitLimit and stopLossMultiplier are now driven by config.json
// WinTarget: 1.20 = 20% gain target; StopLoss: 0.93 = 7% stop loss
// Use c.WinTarget and c.StopLoss throughout (set in goapp.go Config struct).

type Entry struct {
	Symbol    string          `db:"symbol"`
	BuyLimit  float32         `db:"buylimit"`
	SellLimit float32         `db:"selllimit"`
	Volume    float32         `db:"volume"`
	Found     bool            `db:"found"`
	Qty       decimal.Decimal `db:"quantity"`
	MinLow    float32         `db:"minlow"`
	Close     float32         `db:"close"`
	CliffPct  float32         `db:"cliffpct"`
}

func goodEntry(entry Entry) bool {
	// check leverage list.
	// check minVolume
	// check minPrice
	// check duplicate trade from recent trades.

	switch {
	case entry.BuyLimit < c.MinPrice:
		return false
	// case entry.Volume < float32(c.MinVolume):
	// 	return false
	// case leverage[entry.Symbol] > 0:
	// 	return false
	default:
		return true
	}
}

func createPath(path string) error {
	var err error
	dir := filepath.Dir(path)
	err = os.MkdirAll(dir, os.ModePerm)
	_, err = os.Create(path)
	return err
}

// fixSellLimits adds a sell limit order for every position.
// It's probably easier to cancel all existing sellLimits and to add than dig through orders.
func fixSellLimits() error {
	log.Println("fixSellLimits", c.ENV)
	// get all the positions.
	// get all the orders.
	p, err := ac.ListPositions()
	if err != nil {
		return err
	}

	for _, sym := range p {
		dNewSl := (sym.EntryPrice.Mul(decimal.NewFromFloat(float64(c.WinTarget)))).Round(2)
		sell := alpaca.PlaceOrderRequest{
			AccountID:     c.AlpacaAccountNbr,
			AssetKey:      &sym.Symbol,
			Qty:           &sym.Qty,
			Side:          alpaca.Sell,
			Type:          alpaca.Limit,
			TimeInForce:   alpaca.GTC,
			LimitPrice:    &dNewSl,
			ExtendedHours: false,
		}

		resp, err := ac.PlaceOrder(sell) // This should fail for every position that already has a sell limit in place
		if err != nil {
			log.Println(err)
			continue
		}
		spew.Dump(resp)
	}
	return nil
}

func clearStaleBuys() error {
	var err error
	// any buys that haven't filled by the time this function has been called
	log.Println("clearStaleBuys", c.ENV)
	status := "open"
	limit := 20
	nested := false
	buys, err := ac.ListOrders(&status, nil, &limit, &nested)
	if err != nil {
		return err
	}
	if len(buys) < 1 {
		log.Println(len(buys), "No stale buys to clear")
		return nil
	}
	for _, buy := range buys {
		log.Println("Might clear ", buy.Symbol, buy.Status, buy.Side)
		if (buy.Status == "new") && (buy.Side == alpaca.Buy) {
			err = ac.CancelOrder(buy.ID)
			if err != nil {
				log.Println(buy.Symbol, err)
				continue
			}
			log.Println("Cancelled ", buy.Symbol, buy.Qty, buy.Side, buy.ID)
		}
	}

	return nil
}

func clearSellLimits() error {
	status := "all"
	limit := 100
	nested := false
	orders, err := ac.ListOrders(&status, nil, &limit, &nested)
	if err != nil {
		return err
	}
	symbolList := make(map[string]int)
	for _, o := range orders {
		if o.Side == alpaca.Sell {
			symbolList[o.Symbol] = 1
			err = ac.CancelOrder(o.ID)
			if err != nil {
				log.Println("Err canceling ", o.Symbol, o.ID, err)
				continue
			}
		}
	}
	spew.Dump(symbolList)
	return nil
}

func getTradableSymbols() ([]string, error) {
	var symbols []string
	gcsDown(c.RefDbPath)

	db, err := sqlx.Open("sqlite3", c.RefDbPath)
	if err != nil {
		return []string{}, err
	}
	query := "SELECT DISTINCT symbol from SymbolListMinVol100000MinPrice3;"
	query = strings.Replace(query, "SymbolListMinVol100000MinPrice3", c.SymbolTable, 1)
	rows, err := db.Queryx(query)
	if err != nil {
		return []string{}, err
	}
	var symbol string
	for rows.Next() {
		err = rows.Scan(&symbol)
		if err != nil {
			log.Println(err, "while scanning Symbol list")
		}
		symbols = append(symbols, symbol)
	}

	return symbols, db.Close()
}

func loadHistory(limit int, symbols []string, dbpath string) (int, error) {
	create_schema := []string{"sql/backtest_start_schema.sql"}
	runSqlScripts(create_schema, dbpath)

	db, err := sqlx.Open("sqlite3", dbpath)
	if err != nil {
		return 0, err
	}
	client := *md
	//init the loc
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Fatalln("timezone fail", err)
	}
	//set timezone,
	now := time.Now().In(loc) // Solving Error: Entries.go:196: your subscription does not permit querying data from the past 15 minutes

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	s := today.AddDate(0, 0, -limit)
	e := today
	// e := today.AddDate(0, 0, 0) // -1 is
	log.Println("Loading History from ", s, " to ", e)

	datachannel := client.GetMultiBarsAsync(symbols, marketdata.GetBarsParams{
		TimeFrame:  marketdata.NewTimeFrame(1, marketdata.Day),
		Adjustment: marketdata.Split,
		Start:      s,
		End:        e,
	})

	for item := range datachannel {
		if err := item.Error; err != nil {
			log.Fatal(item.Symbol, err)
		}

		result := db.MustExec(
			"INSERT INTO backtest_start (symbol, date, open, high, low, close, volume ) values (?,?,?,?,?,?,?)",
			item.Symbol, item.Bar.Timestamp, item.Bar.Open, item.Bar.High, item.Bar.Low, item.Bar.Close, item.Bar.Volume)
		_, err := result.RowsAffected()
		if err != nil {
			log.Println(err, item.Symbol, item.Bar.Timestamp)
			continue
		}

	}

	return len(symbols) * limit, db.Close()
}

func CalcEntriesInDb() (interface{}, error) {
	symbols, err := getTradableSymbols()
	if err != nil {
		log.Fatal("getTradableSymbols failed.", err)
	}

	log.Println(len(symbols), "symbols to scan.")
	createPath(c.EntryDbPath) // forces a specific path to exist including creation of folders.
	runList := GetRunList(c.RefDbPath)
	////loadHistory because of weekends and holidays lets gather more than 13 days to get enough trail for minlow
	_, err = loadHistory(20, symbols, c.EntryDbPath)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(`Running `, len(runList), `sql scripts.`)
	runSqlScripts(runList, c.EntryDbPath)

	err = gcsUp(c.EntryDbPath)
	htmltable := PrintTableHTML(c.EntryDbPath, "entry")
	sendEmail("Entries Calculated", htmltable, c.EntryDbPath)

	return htmltable, err
}

func enoughBuyingPower() bool {
	// Make sure buying power is enough.
	bp := acct.BuyingPower.IntPart()
	return int64(c.MinBuyingPower) < bp
}

func underPositionCap() bool {
	positions, err := ac.ListPositions()
	if err != nil {
		log.Println("underPositionCap", err)
	}
	if len(positions) >= c.PositionCap {
		return false
	} else {
		return true
	}
}

func underEntryCap() bool {
	// Make sure we haven't already hit max entries for the day
	// c.EntryCap

	status := "closed"
	limit := 10
	nested := false
	buys, err := ac.ListOrders(&status, nil, &limit, &nested)
	if err != nil {
		log.Println(err, "issue with Listing closed orders.")
	}
	if len(buys) < 1 {
		log.Println("No buys to count against EntryCap")
		return true
	}
	year, month, day := time.Now().Date()
	entrycount := 0

	//Throttle entries based on  orders already filled.
	for _, buy := range buys {
		if buy.Side != alpaca.Buy {
			continue
		}
		if buy.FilledAt == nil { // Cancelled buy orders shouldn't be canceled nor other orders that aren't filled.
			continue
		}
		log.Println(spew.Sdump(buy))
		fyear, fmonth, fday := buy.FilledAt.Date()
		if fyear == year && month == fmonth && day == fday {
			entrycount++
		}
	}
	log.Println(entrycount, " entries already filled today.")
	return entrycount < c.EntryCap
}

func genBlockList() map[string]int {
	// Now run through new table entry_stage.
	// 1. Make sure the position doesn't already exist.
	// Make sure it's not already a buy order , too.
	limit := 100
	nested := false
	status := "all"
	orders, err := ac.ListOrders(&status, nil, &limit, &nested)
	if err != nil {
		log.Print(err, "while getting all orders")
	}
	blockList := make(map[string]int)
	for _, o := range orders {
		if o.Side == alpaca.Buy && o.Status != "canceled" {
			blockList[o.Symbol] = 1
		}
	}

	positions, err := ac.ListPositions()
	if err != nil {
		log.Println(err, " while getting positions")
	}
	for _, p := range positions {
		blockList[p.Symbol] = 1
	}
	return blockList
}

func submitEntry(entry *Entry, secondRound *[]Entry) (*alpaca.Order, error) {
	if !goodEntry(*entry) {
		log.Println("Entry doesn't meet filters", *entry)
		return nil, nil
	}
	client := *md

	lp, err := client.GetLatestTrade(entry.Symbol)
	if err != nil {
		log.Println("GetLastTrade", err, lp, entry.Symbol)
		return nil, err
	}

	// if float32(lp.Price) > entry.BuyLimit {

	// 	*secondRound = append(*secondRound, *entry)
	// 	return nil, nil
	// }
	newBl := lp.Price
	newSl := newBl * float64(c.WinTarget)  // 20% take-profit target
	newStop := newBl * float64(c.StopLoss) // 7% stop-loss floor
	dNewBl := decimal.NewFromFloat(newBl).Round(2)
	dNewSl := decimal.NewFromFloat(newSl).Round(2)
	dNewStop := decimal.NewFromFloat(newStop).Round(2)
	sell := alpaca.TakeProfit{
		LimitPrice: &dNewSl,
	}
	stop := alpaca.StopLoss{
		StopPrice: &dNewStop,
	}

	buy := alpaca.PlaceOrderRequest{
		AccountID:     c.AlpacaAccountNbr,
		AssetKey:      &entry.Symbol,
		Qty:           &entry.Qty,
		Side:          alpaca.Buy,
		Type:          alpaca.Limit,
		TimeInForce:   alpaca.GTC,
		LimitPrice:    &dNewBl,
		ExtendedHours: false,
		OrderClass:    alpaca.Bracket,
		TakeProfit:    &sell,
		StopLoss:      &stop,
	}

	buyResp, err := ac.PlaceOrder(buy)
	return buyResp, err
}

func genEntryBlockList() {
	// dirty shortcut is pull all orders that exited on market instead of selllimit.
	// thorough pull down progress sheet generated by python.
	log.Println(c.ProgressSheetId)
	// loop over sheet and for every symbol with a negative

}

func isBlockedEntry(symbol string) bool {
	var blocked = false
	// assume list is already loaded and
	return blocked
}

func ResetSellLimits() error {

	err := clearSellLimits()
	if err != nil {
		log.Println(err, "error clearing Sell Limits")
		return err
	}
	fixSellLimits()
	return nil
}

func SubmitEntries() (interface{}, error) {
	log.Println("Starting SubmitEntries ")
	var err error
	var responses []alpaca.Order
	var secondRound = []Entry{}
	var entrycount int
	initAlpaca()

	err = clearStaleBuys()
	if err != nil {
		log.Println("Issue with clearStaleBuys ", err)
	}

	if !enoughBuyingPower() {
		return "enoughBuyingPower", nil
	}

	if !underEntryCap() {
		return "Enough Entries already", nil
	}

	if !underPositionCap() {
		return "Enough positions already", nil
	}

	gcsDown(c.EntryDbPath)

	entrydb, err := sqlx.Open("sqlite3", c.EntryDbPath)
	if err != nil {
		log.Println("opening c.EntryDbPath failed", err)
		return c.EntryDbPath, err
	}

	blockList := genBlockList()

	entrysql := `
SELECT symbol, qty, buy_limit, sell_limit, found, volume
FROM entry
WHERE found = 1
ORDER BY volume desc;`
	rows, err := entrydb.Query(entrysql)
	if err != nil {
		log.Fatal(err, entrysql)
	}

	for rows.Next() {
		entry := Entry{}
		err = rows.Scan(&entry.Symbol, &entry.Qty, &entry.BuyLimit, &entry.SellLimit, &entry.Found, &entry.Volume)

		switch {
		case err != nil:
			log.Println("Entry Row problem ", err)
			continue
		case blockList[entry.Symbol] > 0:
			log.Println("Already a position or buy order", entry)
			continue
		case !entry.Found:
			log.Println("Entry not flagged as cliff found", entry)
			continue
		default:
			log.Println(entry)
			o, err := submitEntry(&entry, &secondRound)
			if err != nil || o == nil {
				log.Println(err, o)
				continue
			}
			if o.FilledAt != nil {
				entrycount++
			}
			if entrycount >= c.EntryCap {
				return "Hit Entry limit for day", nil
			}
			responses = append(responses, *o)
		}

	}
	// I'm still doing secondRound logic do it here.
	for _, e := range secondRound {
		// This puts buylimits in place in between calls to SubmitEntries.
		var slim = decimal.NewFromFloat32(e.SellLimit).Round(2)
		var blim = decimal.NewFromFloat32(e.BuyLimit).Round(2)
		sell := alpaca.TakeProfit{
			LimitPrice: &slim,
		}

		buy := alpaca.PlaceOrderRequest{
			AccountID:     c.AlpacaAccountNbr,
			AssetKey:      &e.Symbol,
			Qty:           &e.Qty,
			Side:          alpaca.Buy,
			Type:          alpaca.Limit,
			TimeInForce:   alpaca.GTC,
			LimitPrice:    &blim,
			ExtendedHours: false,
			OrderClass:    alpaca.Oto,
			TakeProfit:    &sell,
		}
		buyResp, err := ac.PlaceOrder(buy)
		if err != nil {
			log.Println(err)
			continue
		}
		responses = append(responses, *buyResp)

	}

	// As of 2020-10-14 Alpaca does submit the profit limit on a partially filled order.
	// calling to accomodate partially-filled orders that are missing
	// this will backfire if I enable shortselling on this account.
	err = fixSellLimits()
	return responses, err
}
