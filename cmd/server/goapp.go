package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/alpacahq/alpaca-trade-api-go/v2/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v2/marketdata"
	"github.com/davecgh/go-spew/spew"
	"github.com/jmoiron/sqlx"
	"github.com/olekukonko/tablewriter"
	"golang.org/x/net/context"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"xorm.io/xorm"
)

// Config is config for entire app.
type Config struct {
	ENV                string
	ApcaApiBaseUrl     string
	ServiceAccountJson string
	AlpacaApiKey       string
	AlpacaApiSecret    string
	AlpacaApiBaseUrl   string
	AlpacaAccountNbr   string
	GmailSender        string
	GmailPasswd        string
	GmailRecipient     string
	BucketName         string
	ExitDbPath         string
	EntryDbPath        string
	GcsTopPath         string
	SymbolTable        string
	EntryCap           int
	PositionCap        int
	MinVolume          int
	MinPrice           float32
	MinBuyingPower     float32
	Budget             float32
	TradingDays        int
	WinTarget          float64 // e.g. 1.20 = 20% take-profit target
	StopLoss           float64 // e.g. 0.93 = 7% stop-loss floor
	MaxHoldDays        int     // max days to hold before time-up exit (10)
	SignalThreshold    float64 // minimum win20_10d_rate to qualify (0.90)
	Apikey             string
	EntrySheetId       string
	RefDbPath          string
	TopLocalDir        string
	EntriesLogId       string
	ExitSheetId        string
	SymbolSheetId      string
	MarketSheetId      string
	BigScanStageId     string
	ProgressSheetId    string
	DontExitSheetId    string
	AcctSheetId        string
	OrdersLogId        string
	PortfolioId        string
	PositionsId        string
	LeverageTableId    string
	ExitLogId          string
}

var (
	c           Config
	ac          alpaca.Client
	md          *marketdata.Client
	acct        *alpaca.Account
	exitEngine  *xorm.Engine
	entryEngine *sqlx.DB
	entryXorm   *xorm.Engine
	refEngine   *gorm.DB
	bucket      *storage.BucketHandle
	gcs         *storage.Client
	ctx         context.Context
	leverage    map[string]int
	insert      string
)

func f(err error) {
	if err != nil {
		panic(err)
	}
}

func getAssets() (int, error) {
	status := "active"

	ac.ListAssets(&status)
	assets, err := ac.ListAssets(&status)

	log.Println(len(assets))
	//spew.Dump(assets[0])
	refdb, err := gorm.Open(sqlite.Open(c.RefDbPath), &gorm.Config{})
	if err != nil {
		return 0, err
	}

	refdb.AutoMigrate(&alpaca.Asset{})
	for i, j := 0, 900; i < len(assets); i += 900 {
		j = i + 900
		if j > len(assets) {
			j = len(assets)
		}
		result := refdb.Create(assets[i:j])
		log.Println(result.RowsAffected, " rows affected")
		log.Println(result.Error, " error")
	}

	return len(assets), nil
}

func initConfig() {
	log.Println("Starting initConfig")
	file, err := os.Open("config.json")
	if err != nil {
		log.Fatalln("Could not load config.json ", err)
	}

	decoder := json.NewDecoder(file)
	c = Config{}
	err = decoder.Decode(&c)
	if err != nil {
		log.Println("error:", err)
	}
	//log.Println("ENV: ", c.ENV)
	spew.Dump(c.ENV)
}
func initMarketData() {
	yep := marketdata.NewClient(marketdata.ClientOpts{
		ApiKey:    "PKQKX697LTIJNNOLZ421",
		ApiSecret: "hR0tRhJUX1auSGqD4aqBOlOOdMm5CanouFqJ7IGB",
	})
	md = &yep

}

func initAlpaca() {
	// ac = alpaca.NewClient(alpaca.ClientOpts{
	// 	ApiKey:    c.AlpacaApiKey,
	// 	ApiSecret: c.AlpacaApiSecret,
	// 	BaseURL:   c.AlpacaApiBaseUrl,
	// })
	ac = alpaca.NewClient(alpaca.ClientOpts{
		ApiKey:    "PKQKX697LTIJNNOLZ421",
		ApiSecret: "hR0tRhJUX1auSGqD4aqBOlOOdMm5CanouFqJ7IGB",
		BaseURL:   "https://paper-api.alpaca.markets",
	})
	var err error
	acct, err = ac.GetAccount()
	if err != nil {
		log.Println("initAlpaca")
		log.Fatal(err)
	}

}

func init() {
	var err error
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println("Starting init() ")
	initConfig()
	initAlpaca()
	initMarketData()
	//initLeverage()
	err = initGcs()
	if err != nil {
		log.Fatalf("Failed to init gcs bucket %v", err)
	}
	insert = `
INSERT INTO entry
(
symbol    ,
buylimit  ,
selllimit ,
volume    ,
found     ,
quantity  ,
minlow    ,
close     ,
cliffpct  
)
VALUES (
:symbol,
:buylimit,
:selllimit,
:volume,
:found,
:quantity,
:minlow,
:close,
:cliffpct)
`
}

func GetAllOrders() []alpaca.Order {
	var guess *int
	guess = new(int)
	*guess = 200
	stat := "all"
	orders, err := ac.ListOrdersWithRequest(alpaca.ListOrdersRequest{
		Limit:  guess,
		Status: &stat,
	})
	if err != nil {
		log.Println("GetAllOrdersFailed ", err)
	}
	return orders
}

func OrdersToDB(orders []alpaca.Order, dbpath string) {
	log.Println("OrdersToDB ", len(orders))
	var err error
	dbdir := filepath.Dir(dbpath)
	_ = os.MkdirAll(dbdir, os.ModePerm) //ensure directory exists by creating it.
	os.Remove(dbpath)                   //This is the most blunt way to clear the tables.
	orderengine, err := xorm.NewEngine("sqlite3", dbpath)
	if err != nil {
		log.Fatalln("OrdersToDB failed ", err)
	}
	err = orderengine.Sync2(new(alpaca.Order))
	if err != nil {
		log.Fatalln("OrdersToDB", dbpath, err)
	}
	counts, err := orderengine.Insert(orders)
	if err != nil {
		log.Println(err)
	}
	log.Println("OrdersToDB ", counts)

	// db, err := gorm.Open(sqlite.Open(dbpath), &gorm.Config{
	// 	DisableForeignKeyConstraintWhenMigrating: true,
	// })
	// if err != nil {
	// 	log.Fatalln("Opening db failed ", dbpath, err)
	// }
	// db.AutoMigrate(&orders[0])
	// db.Create(&orders)

}

func runSqlScripts(runList []string, dbpath string) {
	database, err := sqlx.Open("sqlite3", dbpath)
	if err != nil {
		log.Println(err)
		return
	}
	for _, file := range runList {
		sqlScript, err := ioutil.ReadFile(file)
		if err != nil {
			log.Println("issue reading sql script", err)
		}
		if _, err := database.Exec(string(sqlScript)); err != nil {
			log.Printf("%v", file)
			log.Printf("%v", err)

		} else {
			log.Println("Success ", file)
		}
	}
}

func GetRunList(refdb string) []string {

	db := sqlx.MustConnect("sqlite3", refdb)
	defer db.Close()
	// instead of using colunm list_folder hardcoding to sql/
	qry := `select 'sql/'|| sql_file as script from run_list 
	where run_order > 0 and daily_scan = 1
	ORDER BY run_order;`

	scripts, err := db.Query(qry)
	if err != nil {
		println("script qry failed ", qry)
	}
	defer scripts.Close()
	var files []string
	var file string
	for scripts.Next() {
		err = scripts.Scan(&file)
		if err != nil {
			println(err)

		} else {
			files = append(files, file)
		}
	}
	return files
}

type SymbolPriceVolume struct {
	Symbol string
	Price  float64
	Volume uint64
}

// Really wish I had documented this function updatePriceVolume
func updatePriceVolume() (int, error) {
	refdb, err := gorm.Open(sqlite.Open(c.RefDbPath), &gorm.Config{})
	if err != nil {
		return 0, err
	}
	refdb.AutoMigrate(&SymbolPriceVolume{})

	var assets []string
	db, err := sqlx.Open("sqlite3", c.RefDbPath)
	results, err := db.Queryx("Select distinct symbol from leveraged_etf")
	if err != nil {
		log.Fatal(err)
	}
	for results.Next() {
		var symbol string
		results.Scan(&symbol)
		assets = append(assets, symbol)
	}
	//refdb.Select("symbol").Find(&assets)
	log.Println("Getting volume price for ", assets)
	params := marketdata.GetBarsParams{
		TimeFrame:  marketdata.NewTimeFrame(1, marketdata.Hour),
		Adjustment: marketdata.Split,
		TotalLimit: 1,
	}
	for i := 0; i < len(assets); i += 100 {
		j := i + 100
		client := *md

		bars, err := client.GetMultiBars(assets[i:j], params)
		if err != nil {
			log.Println(err)
		}

		for sym, bar := range bars {
			if len(bar) != 1 {
				continue
			}
			refdb.Create(
				SymbolPriceVolume{
					Symbol: sym,
					Price:  bar[0].Close,
					Volume: bar[0].Volume,
				})
		}
	}
	return len(assets), nil
}

func updateTradeList() (int64, error) {
	gcsDown(c.RefDbPath)

	log.Println("updateTradeList")
	db, err := sqlx.Open("sqlite3", c.RefDbPath)
	if err != nil {
		log.Println(err, "Fail opening", c.RefDbPath)
	}
	log.Println(db.Exec("DROP TABLE IF EXISTS SymbolListMinVol100000MinPrice3;"))
	log.Println(db.Exec("DROP TABLE IF EXISTS assets;"))
	log.Println(db.Exec("DROP TABLE IF EXISTS symbol_price_volumes;"))
	log.Println(db.Exec("DROP TABLE IF EXISTS Leverage;"))

	// Create Leverage table
	scriptbytes, err := ioutil.ReadFile("sql/Leverage.sql")
	affected, err := db.Exec(string(scriptbytes))
	if err != nil {
		log.Println("issue running Leverage.sql  ", err)
		return 0, err
	}

	log.Println(getAssets()) // Still need to fix assets clear table before
	// log.Println(updatePriceVolume()) // commented out to see if i can scan stuff working without these issues.
	// Run script to join Leverage, volume , price requirements.
	scriptbytes, err = ioutil.ReadFile("sql/create_symbol_list_from_symbol_price_volume.sql")
	affected, err = db.Exec(string(scriptbytes))
	if err != nil {
		log.Println("issue running  create_symbol_list_from_symbol_price_volume", err)
		return 0, err
	}
	spew.Dump(affected.RowsAffected())
	log.Println("Expect SymbolListMinVol100000MinPrice3 to be updated.")
	err = gcsUp(c.RefDbPath)
	if err != nil {
		log.Println("issue uploading ", c.RefDbPath, err)
	}
	return affected.RowsAffected()
}

func PrintTableHTML(dbpath string, table string) string {
	tbsql := `SELECT * FROM '` + table + `';`
	db, err := sqlx.Open("sqlite3", dbpath)
	if err != nil {
		log.Fatalln("PrintTable failed", err)
	}

	result, err := db.Queryx(tbsql)
	if err != nil {
		log.Fatalln("print query failed", err)
	}

	cols, err := result.Columns()
	if err != nil {
		log.Fatalln(err)
	}
	var tw [][]string
	tw = append(tw, cols)

	for result.Next() {
		row, err := result.SliceScan()
		if err != nil {
			log.Println(err)

		}
		s := make([]string, len(row))
		for i, v := range row {
			s[i] = fmt.Sprint(v)
		}
		tw = append(tw, s)
	}

	t := template.Must(template.New("").Parse(`<table>{{range .}}<tr>{{range .}}<td>{{.}}</td>{{end}}</tr>{{end}}</table>`))

	var body bytes.Buffer
	if err := t.Execute(&body, tw); err != nil {
		log.Fatal(err)
	}
	return body.String()
}

// I dont have a separate file for all things sqlite related.
// ydea change this to use my query tool in flight where you just pass in a query path and it returns the query as html.
func PrintTable(dbpath string, table string) string {

	tbsql := `SELECT * FROM ` + table
	db, err := sqlx.Open("sqlite3", dbpath)
	if err != nil {
		log.Fatalln("PrintTable failed", err)
	}

	result, err := db.Queryx(tbsql)
	if err != nil {
		log.Fatalln("print query failed", err)
	}

	cols, err := result.Columns()
	if err != nil {
		log.Fatalln(err)
	}

	tableString := &strings.Builder{}
	tw := tablewriter.NewWriter(tableString)

	tw.SetHeader(cols)
	for result.Next() {
		cols, err := result.SliceScan()
		if err != nil {
			log.Println(err)

		}
		s := make([]string, len(cols))
		for i, v := range cols {
			s[i] = fmt.Sprint(v)
		}
		tw.Append(s)
	}
	tw.Render()
	return (tableString.String())
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	spew.Dump(r.URL.Query())
	params := r.URL.Query()
	var apikey string
	log.Println("X-Appengine-Cron", r.Header["X-Appengine-Cron"])
	//log.Println(params["marina"])
	if len(params["marina"]) == 1 {
		apikey = params["marina"][0]
	}

	if len(r.Header["X-Appengine-Cron"]) > 0 || apikey == c.Apikey {
		log.Println("Key or Param passed.")
	} else {
		http.NotFound(w, r)
		return
	}
	//spew.Dump(r.URL.Path)
	switch r.URL.Path {
	case "/calcentriesindb":
		resp, err := CalcEntriesInDb()
		if err != nil {
			_, err = fmt.Fprint(w, err)
		} else {
			_, err = spew.Fprint(w, resp)
		}
	case "/calcstageexits":
		resp, err := CalcExits()
		if err != nil {
			_, err = fmt.Fprint(w, err)
		} else {
			_, err = spew.Fprint(w, resp)
		}
	case "/clearstalebuys":
		err := clearStaleBuys()
		resp := "Cleared Stale Buys"
		if err != nil {
			fmt.Fprint(w, err)
		} else {
			spew.Fprint(w, resp)
		}
	case "/fixselllimits":
		err := fixSellLimits()
		resp := "Fixed Sell Limits"
		if err != nil {
			_, err = fmt.Fprint(w, err)
		} else {
			_, err = spew.Fprint(w, resp)
		}
	case "/submitexits":
		resp, err := SubmitExits()
		if err != nil {
			_, err = fmt.Fprint(w, err)
		} else {
			_, err = spew.Fprint(w, resp)
		}
	case "/submitentries":
		resp, err := SubmitEntries()
		if err != nil {
			_, err = fmt.Fprint(w, err)
		} else {
			_, err = spew.Fprint(w, resp)

		}
	case "/updatetradelist":
		resp, err := updateTradeList()
		if err != nil {
			_, err = fmt.Fprint(w, err)
		} else {
			_, err = spew.Fprint(w, "updatetradelist", resp)

		}
	case "/sendemail":
		sendEmail("Testing new email", "Sqlite attached", c.EntryDbPath) // testing gmail.
		fmt.Fprint(w, "Testing new email", "Sqlite attached", c.EntryDbPath)

	case "/sendgaemail":
		resp, err := gaeEmail(r)
		fmt.Fprint(w, resp, err)
	case "/getallorders":
		orders := GetAllOrders()
		sqpath := filepath.Join(c.TopLocalDir, "report.sqlite")
		createPath(sqpath)
		OrdersToDB(orders, sqpath)
		fmt.Fprint(w, PrintTableHTML(sqpath, "order"))
	case "/scansignals":
		// Runs pre-market daily scan. Fires entries when qualifying 90% signals found.
		resp, err := ScanSignals()
		if err != nil {
			fmt.Fprint(w, err)
		} else {
			spew.Fprint(w, resp)
		}
	default:
		http.NotFound(w, r)
		return
	}
}

// ScanSignals runs the WC signal pipeline against fresh intraday data,
// queries backtested_win_20_10d for today's qualifying symbols,
// sends an email alert, and submits entries via the existing Alpaca flow.
// Reuses: GetRunList, runSqlScripts, loadHistory, sendEmail, SubmitEntries.
func ScanSignals() (interface{}, error) {
	log.Println("Starting ScanSignals()")

	symbols, err := getTradableSymbols()
	if err != nil {
		log.Println("ScanSignals: getTradableSymbols failed", err)
		return nil, err
	}

	createPath(c.EntryDbPath)
	// Load enough history for the trailing minlow calculation (20 days covers weekends/holidays)
	_, err = loadHistory(20, symbols, c.EntryDbPath)
	if err != nil {
		log.Println("ScanSignals: loadHistory failed", err)
		return nil, err
	}

	runList := GetRunList(c.RefDbPath)
	log.Println("ScanSignals: running", len(runList), "sql scripts")
	runSqlScripts(runList, c.EntryDbPath)

	// Query which symbols fired the WC signal AND are in the 90% qualifying list
	entrydb, err := sqlx.Open("sqlite3", c.EntryDbPath)
	if err != nil {
		return nil, err
	}
	defer entrydb.Close()

	var qualifying []string
	err = entrydb.Select(&qualifying, `
		SELECT e.symbol
		FROM entry e
		JOIN backtested_win_20_10d q ON q.symbol = e.symbol
		WHERE e.found = 1
		  AND q.win20_10d_rate >= ?
		ORDER BY q.win20_10d_rate DESC
	`, c.SignalThreshold)
	if err != nil {
		log.Println("ScanSignals: qualifying query failed", err)
		return nil, err
	}

	log.Println("ScanSignals:", len(qualifying), "qualifying signals at >=90% threshold:", qualifying)

	if len(qualifying) > 0 {
		sendEmail(
			"🚨 WC Signal Fired — 20% in 10d target",
			fmt.Sprintf("Qualifying symbols: %v\n\nWin threshold: %.0f%%\nTarget: +%.0f%% | Stop: -%.0f%%",
				qualifying,
				c.SignalThreshold*100,
				(c.WinTarget-1)*100,
				(1-c.StopLoss)*100,
			),
			c.EntryDbPath,
		)
		return SubmitEntries()
	}

	return fmt.Sprintf("ScanSignals: no qualifying signals today (%d symbols scanned)", len(symbols)), nil
}

func hostMode() {
	http.HandleFunc("/", indexHandler)

	// [START setting_port]
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func main() {
	//log.Println(clearStaleBuys())
	//log.Println(CalcExits())
	//log.Println(SubmitExits())
	//log.Println(fixSellLimits())
	// sendEmail("Testing new email", "Sqlite attached", c.EntryDbPath) // testing gmail.

	//log.Println(SubmitEntries())
	//log.Println(gcsUp(c.EntryDbPath))
	//log.Println(gcsDown("/Morningstar/quick_star.csv"))

	//readSheet(c.EntrySheetId)
	//readSheet("1QV5wMkCh58icXfcMgS69xlq174-nti5-PcMSpCmJHdk")

	//log.Println(CalcEntries())
	// log.Println(CalcEntriesInDb())
	//log.Println(updateTradeList())
	//log.Println(gcsUp(c.RefDbPath))
	//log.Println(getTradableSymbols())

	hostMode()

}
