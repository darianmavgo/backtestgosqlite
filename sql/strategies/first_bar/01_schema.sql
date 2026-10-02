-- Buy one symbol on the first bar of the run and never sell. __TRADE_SYMBOL__ is
-- the symbol and __START_DATE__ / __END_DATE__ are the window of bars loaded.
DROP TABLE IF EXISTS first_bar_signals;
CREATE TABLE first_bar_signals (
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
