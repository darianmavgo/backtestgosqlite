/*
 Navicat Premium Data Transfer

 Source Server         : history.db
 Source Server Type    : SQLite
 Source Server Version : 3030001
 Source Schema         : main

 Target Server Type    : SQLite
 Target Server Version : 3030001
 File Encoding         : 65001

 Date: 15/07/2022 11:25:46
*/

PRAGMA foreign_keys = false;

-- ----------------------------
-- Table structure for backtest_start
-- ----------------------------
DROP TABLE IF EXISTS "backtest_start";
CREATE TABLE "backtest_start" (
  "idx" integer,
  "Date" DATETIME,
  "open" FLOAT,
  "high" FLOAT,
  "low" FLOAT,
  "close" FLOAT,
  "Adj Close" FLOAT,
  "volume" BIGINT,
  "symbol" TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_backtest_start_unique ON backtest_start(symbol, Date);
CREATE INDEX IF NOT EXISTS idx_backtest_start_sym_date ON backtest_start(symbol, Date);

PRAGMA foreign_keys = true;
