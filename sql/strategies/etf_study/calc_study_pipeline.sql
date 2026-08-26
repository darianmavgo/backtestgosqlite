-- Schema and queries for Top 5 S&P 500 ETFs 4-Day Position Study
CREATE UNIQUE INDEX IF NOT EXISTS idx_backtest_start_unique ON backtest_start(symbol, Date);
CREATE INDEX IF NOT EXISTS idx_backtest_start_sym_date ON backtest_start(symbol, Date);

DROP TABLE IF EXISTS study_buy_signals;
CREATE TABLE IF NOT EXISTS study_buy_signals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume BIGINT,
    rsi5 REAL,
    sma20 REAL,
    signal_type TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_study_signals_sym_date ON study_buy_signals(symbol, date);

DROP TABLE IF EXISTS study_trades;
CREATE TABLE IF NOT EXISTS study_trades (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    symbol TEXT,
    entry_idx INTEGER,
    entry_date TEXT,
    entry_price REAL,
    exit_idx INTEGER,
    exit_date TEXT,
    exit_price REAL,
    hold_days INTEGER,
    shares INTEGER,
    invested_capital REAL,
    gross_pnl REAL,
    net_pnl REAL,
    return_pct REAL,
    is_win INTEGER,
    mae_pct REAL,
    mfe_pct REAL,
    exit_reason TEXT DEFAULT '4_DAY_TIME_EXIT',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_study_trades_sym ON study_trades(symbol, entry_date);

DROP TABLE IF EXISTS study_win_rates;
CREATE TABLE IF NOT EXISTS study_win_rates (
    symbol TEXT PRIMARY KEY,
    total_trades INTEGER,
    winning_trades INTEGER,
    losing_trades INTEGER,
    win_rate REAL,
    win_rate_pct TEXT,
    profit_factor REAL,
    payoff_ratio REAL,
    avg_win_amount REAL,
    avg_loss_amount REAL,
    avg_trade_return_pct REAL,
    net_profit REAL,
    total_invested REAL,
    avg_mae_pct REAL,
    avg_mfe_pct REAL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

DROP TABLE IF EXISTS study_horizon_comparison;
CREATE TABLE IF NOT EXISTS study_horizon_comparison (
    hold_days INTEGER,
    total_trades INTEGER,
    winning_trades INTEGER,
    losing_trades INTEGER,
    win_rate REAL,
    win_rate_pct TEXT,
    profit_factor REAL,
    avg_trade_return_pct REAL,
    total_net_profit REAL,
    avg_mae_pct REAL,
    avg_mfe_pct REAL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
