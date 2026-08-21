package main

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v2/alpaca"
	"github.com/davecgh/go-spew/spew"
	"github.com/jmoiron/sqlx"
	"github.com/shopspring/decimal"
	"xorm.io/xorm"
)

type Exit struct {
	Symbol string
	TimeUp int
	Qty    int
}

func getCal() []alpaca.CalendarDay {

	rn := time.Now()
	bf := rn.AddDate(0, 0, -30).Format(time.RFC3339)
	af := rn.AddDate(0, 0, 5).Format(time.RFC3339)
	spew.Dump(bf, af)
	cal, err := ac.GetCalendar(&bf, &af)
	if err != nil {
		panic(err)
	}
	if len(cal) < 1 {
		log.Fatal("Calendar returned no days")
	}
	count, err := exitEngine.Insert(cal)
	log.Println(count, " cal_days added")
	f(err)
	spew.Dump(count)
	return cal
}

func clearExitDb(dbPath string) error {
	var err error
	db, err := sqlx.Connect("sqlite3", dbPath)
	if err != nil {
		log.Println("Failed connecting ", dbPath, err)
		return err
	}
	tables := []string{
		"order",
		"position",
		"exit_dates",
		"entry_dates",
		"exit_stage",
		"exit_nums",
		"calendar_day",
		"trading_days",
		"time_up",
	}

	for _, table := range tables {
		result, err := db.Exec("DROP TABLE IF EXISTS ?", table)
		if err != nil {
			log.Println("Failed dropping ", table)
		}
		log.Println("Dropped ", table)
		log.Println(result)
	}
	return nil
}

func createTables(engine *xorm.Engine) error {
	var err error
	err = engine.Sync2(new(alpaca.Position))
	err = engine.Sync2(new(alpaca.Order))
	err = engine.Sync2(new(alpaca.CalendarDay))
	if err != nil {
		log.Println("Failure during table creation Positon, Order, CalendarDay ", err)
		return err
	}
	return nil
}

func CalcExits() (interface{}, error) {
	log.Println("Starting CalcExits()")
	var err error
	dbdir := filepath.Dir(c.ExitDbPath)
	_ = os.MkdirAll(dbdir, os.ModePerm) //ensure directory exists by creating it.
	os.Remove(c.ExitDbPath)             //This is the most blunt way to clear the tables.
	exitEngine, err = xorm.NewEngine("sqlite3", c.ExitDbPath)
	// err = clearExitDb(c.ExitDbPath) // this is probably redundant if I'm using os to remove.
	err = createTables(exitEngine)
	if err != nil {
		log.Println("CalcExits", c.ExitDbPath, err)
		return nil, err
	}
	positions, err := ac.ListPositions()
	if err != nil {
		log.Println("Failed with listPositions()", err)

		panic(err)
	}
	counts, _ := exitEngine.Insert(positions) //Using orm to get Inserts going
	log.Println(counts, " positions added")

	status := "all"
	limit := 1000
	nested := true
	orders, err := ac.ListOrders(&status, nil, &limit, &nested)
	if err != nil {
		panic(err)
	}
	counts, err = exitEngine.Insert(orders)
	log.Println(counts, err, " orders inserted")
	_ = getCal() // need a better name since this also stages to table.
	var runList = []string{
		"sql/trading_days.sql",
		"sql/entry_dates.sql",
		"sql/exit_nums.sql",
		"sql/exit_nums_old_entries.sql",
		"sql/time_up.sql",
		"sql/exit_stage.sql",
	}
	runSqlScripts(runList, c.ExitDbPath)

	// by now there should be a table exits_stage with only the actual symbols to
	// exit Instead of precomputing sell limits that need to be cancelled; submit
	// exits will fetch orders with symbol related

	err = gcsUp(c.ExitDbPath)
	if err != nil {
		log.Println("Failed to upload to gcs", err)
	}
	htmltable := PrintTableHTML(c.ExitDbPath, "exit_stage")
	sendEmail("Exits Calculated", htmltable, c.ExitDbPath)
	return htmltable, err
}

func genPositionSymbols() map[string]decimal.Decimal {
	var positions = make(map[string]decimal.Decimal)
	ps, err := ac.ListPositions()
	if err != nil {
		log.Println(err, "Failed list positions")
	}
	for _, p := range ps {
		positions[p.Symbol] = p.Qty
	}
	return positions
}

func genExitSymbols() map[string]int {
	var exits = make(map[string]int)

	gcsDown(c.ExitDbPath) // just thought of issue.  what happens when engine is created before file is pulled down?
	database, err := sql.Open("sqlite3", c.ExitDbPath)
	if err != nil {
		log.Fatalln("database open fail SubmitExits", err)
		return exits
	}

	rows, err := database.Query("select symbol, qty, time_up from exit_stage where time_up = 1;")
	if err != nil {
		log.Fatalln("exit query failed", err)
		return exits
	}

	for rows.Next() {
		var e Exit
		err := rows.Scan(&e.Symbol, &e.Qty, &e.TimeUp)
		if err != nil {
			log.Println(err, "Bad exit row")
			continue
		}
		if e.TimeUp != 1 {
			continue
		}
		exits[e.Symbol]++
	}

	return exits
}

func SubmitExits() (string, error) {
	log.Println("Starting SubmitExits()")
	var err error
	initConfig()
	initAlpaca()
	exits := genExitSymbols()
	positions := genPositionSymbols()

	// Should find a cleaner way for setting up listOrders
	status := "all"
	limit := 100
	nested := true
	// since I can't set market orders on positions that already have a pending sell order
	// I first have to cancel a bunch of sell limit orders to submit the exit order.
	cancelCandidates, err := ac.ListOrders(&status, nil, &limit, &nested)
	if err != nil {
		log.Fatalln(err, "while get orders to potentially cancel")
		return "", err
	}

	//
	for _, o := range cancelCandidates {
		if exits[o.Symbol] < 1 {
			continue
		}
		// Gonna see if not checking for new or open gets the pending_cancel status
		//if !(o.Status == "new" || o.Status == "open") {
		//	continue
		//}

		log.Println("Canceling ", o.ID, o.Symbol)
		err := ac.CancelOrder(o.ID)
		if err != nil {
			// without checking order status I'll probably try to cancel orders that already happened.  oh well.
			log.Println(err, "Cancel failed", spew.Sdump(o))
			continue
		}
	}

	for sym := range exits {
		q := positions[sym]
		newOrder, err := ac.PlaceOrder(alpaca.PlaceOrderRequest{
			AccountID:   c.AlpacaAccountNbr,
			AssetKey:    &sym,
			Qty:         &q,
			Side:        alpaca.Sell,
			Type:        alpaca.Market,
			TimeInForce: alpaca.Day,
		})
		if err != nil {
			log.Println("Market exit failed", err, sym)
		}

		log.Println("Market exit", spew.Sdump(newOrder))
	}

	return spew.Sdump(exits), nil
}
