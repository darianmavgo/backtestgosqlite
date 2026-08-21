/*
 Navicat Premium Data Transfer

 Source Server         : sanddunestudy
 Source Server Type    : SQLite
 Source Server Version : 3030001
 Source Schema         : main

 Target Server Type    : SQLite
 Target Server Version : 3030001
 File Encoding         : 65001

 Date: 09/06/2022 11:40:19
*/

PRAGMA foreign_keys = false;

-- ----------------------------
-- Table structure for wc_buy_signal_slice
-- ----------------------------
DROP TABLE IF EXISTS "wc_buy_signal_slice";
CREATE TABLE "wc_buy_signal_slice" (
  "idx" INT,
  "buy_signal" integer
);

PRAGMA foreign_keys = true;
