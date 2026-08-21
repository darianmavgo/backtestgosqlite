/*
 Navicat Premium Data Transfer

 Source Server         : settings.db
 Source Server Type    : SQLite
 Source Server Version : 3030001
 Source Schema         : main

 Target Server Type    : SQLite
 Target Server Version : 3030001
 File Encoding         : 65001

 Date: 27/07/2022 12:08:17
*/

PRAGMA foreign_keys = false;

-- ----------------------------
-- Table structure for leveraged_etf
-- ----------------------------
DROP TABLE IF EXISTS "leveraged_etf";
CREATE TABLE "leveraged_etf" (
  "symbol" TEXT(255),
  "provider" TEXT(255),
  "leverage" TEXT(255),
  "volume_recent" TEXT(255)
);

PRAGMA foreign_keys = true;
