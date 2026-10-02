-- Trade the prior calendar year's best performer for the new year. __SIDE__ is
-- long, short or inverse. __START_DATE__ and __END_DATE__ are the window of bars
-- loaded. Each file after this one reads the table the one before it wrote.
DROP TABLE IF EXISTS aw_bar;
CREATE TABLE aw_bar (
    idx INTEGER, symbol TEXT, year TEXT, date TEXT,
    open REAL, high REAL, low REAL, close REAL, volume INTEGER,
    rn_first INTEGER, rn_last INTEGER
);
CREATE INDEX IF NOT EXISTS idx_aw_bar ON aw_bar(symbol, year, date);

DROP TABLE IF EXISTS aw_year;
CREATE TABLE aw_year (
    symbol TEXT, year TEXT, first_date TEXT, first_open REAL, last_date TEXT, last_close REAL, ret REAL
);
CREATE INDEX IF NOT EXISTS idx_aw_year ON aw_year(year, symbol);

DROP TABLE IF EXISTS aw_pick;
CREATE TABLE aw_pick (year TEXT, symbol TEXT, ret REAL);

DROP TABLE IF EXISTS aw_trade;
CREATE TABLE aw_trade (year TEXT, trade_symbol TEXT, entry_date TEXT);

DROP TABLE IF EXISTS aw_inverse;
CREATE TABLE aw_inverse (symbol TEXT PRIMARY KEY, inverse TEXT);

DROP TABLE IF EXISTS annual_winner_signals;
CREATE TABLE annual_winner_signals (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    buylimit REAL,
    entry INTEGER,
    direction TEXT,
    regime TEXT,
    hold_days_override INTEGER,
    take_profit REAL,
    stop_loss REAL,
    allocation_pct_override REAL
);
