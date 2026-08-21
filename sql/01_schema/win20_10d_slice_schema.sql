-- win20_10d_slice_schema.sql
-- Stores the Win20_10d flag and max gain pct for each idx.
-- Win20_10d = 1 means price reached +20% above close within the next 10 trading days.

DROP TABLE IF EXISTS "win20_10d_slice";
CREATE TABLE "win20_10d_slice" (
  "idx"          INT,
  "Win20_10d"    INTEGER,   -- 1 if maxhigh10 > close * 1.20, else 0
  "max_gain_10d" REAL       -- (maxhigh10 - close) / close
);
