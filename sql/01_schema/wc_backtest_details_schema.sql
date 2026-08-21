/*
 Extended from original wc_backtest_details_schema.sql.
 Added: Win20_10d, max_gain_10d, maxhigh10 columns for 20%-in-10-days strategy.
*/

PRAGMA foreign_keys = false;

DROP TABLE IF EXISTS "wc_backtest_details";
CREATE TABLE "wc_backtest_details" (
  "idx"          INT,
  "symbol"       TEXT,
  "date"         NUM,
  "open"         REAL,
  "high"         REAL,
  "low"          REAL,
  "close"        REAL,
  "volume"       INT,
  "buylimit"     REAL,
  "entry"        INTEGER,
  "minlow4"      REAL,
  "maxhigh4"     REAL,
  "Win5"         INT,
  "Win3"         INT,
  "lowgap"       REAL,
  "highgap"      REAL,
  "lowgappct"    REAL,
  "highgappct"   REAL,
  -- NEW: 10-day 20% win columns
  "maxhigh10"    REAL,    -- highest high in next 10 trading days
  "Win20_10d"    INTEGER, -- 1 if maxhigh10 > close * 1.20
  "max_gain_10d" REAL     -- (maxhigh10 - close) / close
);

PRAGMA foreign_keys = true;
