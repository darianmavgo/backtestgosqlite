

PRAGMA foreign_keys = false;

-- ----------------------------
-- Table structure for leveraged_etf
-- ----------------------------
DROP TABLE IF EXISTS "handpicked";
CREATE TABLE "handpicked" (
  "symbol" TEXT(255),
  "volume_recent" integer,
  "price_recent" real
);

PRAGMA foreign_keys = true;
