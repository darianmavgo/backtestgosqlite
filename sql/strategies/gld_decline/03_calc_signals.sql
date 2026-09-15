-- Generate Buy Signals on GLD when declining __DECLINE_DAYS__ days in a row (default 2, see StrategyConfig.DeclineDays)
INSERT INTO gld_decline_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
SELECT
    idx,
    'GLD' AS symbol,
    date,
    open,
    high,
    low,
    close,
    volume,
    close AS buylimit,
    1 AS entry,
    'LONG' AS direction,
    'All Regimes' AS regime,
    __HOLD_DAYS__ AS hold_days_override,
    close * __TAKE_PROFIT_MULT__ AS take_profit,
    close * __STOP_LOSS_MULT__ AS stop_loss
FROM gld_streaks_slice
WHERE down_streak >= __DECLINE_DAYS__;
