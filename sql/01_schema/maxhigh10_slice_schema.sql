-- maxhigh10_slice_schema.sql
-- Mirror of maxhigh4_slice_schema.sql extended to 10 trading days forward.
-- Stores the highest high price reached in the 10 trading days after each idx.

DROP TABLE IF EXISTS "maxhigh10_slice";
CREATE TABLE "maxhigh10_slice" (
  "idx"       INT,
  "maxhigh10" REAL
);
