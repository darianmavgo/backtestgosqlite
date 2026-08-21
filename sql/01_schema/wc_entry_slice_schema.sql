/*
 Navicat Premium Data Transfer

 Source Server         : history
 Source Server Type    : SQLite
 Source Server Version : 3030001
 Source Schema         : main

 Target Server Type    : SQLite
 Target Server Version : 3030001
 File Encoding         : 65001

 Date: 11/07/2022 18:40:58
*/

PRAGMA foreign_keys = false;

-- ----------------------------
-- Table structure for wc_entry_slice
-- ----------------------------
DROP TABLE IF EXISTS "wc_entry_slice";
CREATE TABLE "wc_entry_slice" (
  "idx" INT,
  "buylimitmet" integer
);

PRAGMA foreign_keys = true;
