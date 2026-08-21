DROP TABLE IF EXISTS "entry";
CREATE TABLE `entry`
(
    `symbol`     TEXT    NULL,
    `date`       TEXT    NULL,
    `buy_limit`  REAL    NULL,
    `sell_limit` REAL    NULL,
    `volume`     REAL    NULL,
    `found`      INTEGER NULL,
    `qty`        INTEGER NULL,
    `min_low`    REAL    NULL,
    `close`      REAL    NULL,
    `cliff_pct`  REAL    NULL
);
