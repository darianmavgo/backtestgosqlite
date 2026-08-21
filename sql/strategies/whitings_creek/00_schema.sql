-- Consolidated Schema for Whitings Creek Strategy Pipeline
DROP TABLE IF EXISTS entry;
CREATE TABLE IF NOT EXISTS entry (
    symbol     TEXT NULL,
    date       TEXT NULL,
    buy_limit  REAL NULL,
    sell_limit REAL NULL,
    volume     REAL NULL,
    found      INTEGER NULL,
    qty        INTEGER NULL,
    min_low    REAL NULL,
    close      REAL NULL,
    cliff_pct  REAL NULL
);

DROP TABLE IF EXISTS maxhigh10_slice;
CREATE TABLE IF NOT EXISTS maxhigh10_slice (
    idx       INTEGER,
    maxhigh10 REAL
);

DROP TABLE IF EXISTS maxhigh4_slice;
CREATE TABLE IF NOT EXISTS maxhigh4_slice (
    idx     INTEGER,
    maxhigh REAL
);

DROP TABLE IF EXISTS minlow4_slice;
CREATE TABLE IF NOT EXISTS minlow4_slice (
    idx    INTEGER,
    minlow REAL
);

DROP TABLE IF EXISTS wc_buy_signal_slice;
CREATE TABLE IF NOT EXISTS wc_buy_signal_slice (
    idx        INTEGER,
    buy_signal INTEGER
);

DROP TABLE IF EXISTS wc_entry_slice;
CREATE TABLE IF NOT EXISTS wc_entry_slice (
    idx         INTEGER,
    buylimitmet INTEGER
);

DROP TABLE IF EXISTS wc_trailing_minlow_slice;
CREATE TABLE IF NOT EXISTS wc_trailing_minlow_slice (
    idx    INTEGER,
    minlow REAL
);

DROP TABLE IF EXISTS win20_10d_slice;
CREATE TABLE IF NOT EXISTS win20_10d_slice (
    idx          INTEGER,
    Win20_10d    INTEGER,
    max_gain_10d REAL
);

DROP TABLE IF EXISTS win_slice;
CREATE TABLE IF NOT EXISTS win_slice (
    idx  INTEGER,
    Win5 INTEGER,
    Win3 INTEGER
);

DROP TABLE IF EXISTS wc_backtest_details;
CREATE TABLE IF NOT EXISTS wc_backtest_details (
    idx          INTEGER,
    symbol       TEXT,
    date         TEXT,
    open         REAL,
    high         REAL,
    low          REAL,
    close        REAL,
    volume       INTEGER,
    buylimit     REAL,
    entry        INTEGER,
    minlow4      REAL,
    maxhigh4     REAL,
    Win5         INTEGER,
    Win3         INTEGER,
    lowgap       REAL,
    highgap      REAL,
    lowgappct    REAL,
    highgappct   REAL,
    maxhigh10    REAL,
    Win20_10d    INTEGER,
    max_gain_10d REAL
);
CREATE INDEX IF NOT EXISTS idx_wc_details ON wc_backtest_details (symbol, date);
