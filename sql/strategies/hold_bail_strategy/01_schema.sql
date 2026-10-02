-- Hold one symbol and re-enter when its close is above its simple moving
-- average. __TRADE_SYMBOL__ is the symbol, __SMA_PERIOD__ the average's length,
-- __SMA_PRECEDING__ that length minus one, and __START_DATE__ / __END_DATE__ the
-- window of bars loaded.
DROP TABLE IF EXISTS hold_bail_sma;
CREATE TABLE hold_bail_sma (
    idx INTEGER, symbol TEXT, date TEXT,
    open REAL, high REAL, low REAL, close REAL, volume INTEGER,
    rn INTEGER, sma REAL
);

DROP TABLE IF EXISTS hold_bail_strategy_signals;
CREATE TABLE hold_bail_strategy_signals (
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
