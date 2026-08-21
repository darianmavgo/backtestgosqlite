/*
 Navicat Premium Data Transfer

 Source Server         : history
 Source Server Type    : SQLite
 Source Server Version : 3030001
 Source Schema         : main

 Target Server Type    : SQLite
 Target Server Version : 3030001
 File Encoding         : 65001

 Date: 11/07/2022 19:53:46
*/

PRAGMA foreign_keys = false;

-- ----------------------------
-- Table structure for win_slice
-- ----------------------------
DROP TABLE IF EXISTS "win_slice";
CREATE TABLE "win_slice" (
  "idx" INT,
  "Win5" integer,
  "Win3" integer
);

PRAGMA foreign_keys = true;
