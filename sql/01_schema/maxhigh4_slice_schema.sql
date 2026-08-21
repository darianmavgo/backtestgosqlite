/*
 Navicat Premium Data Transfer

 Source Server         : history
 Source Server Type    : SQLite
 Source Server Version : 3030001
 Source Schema         : main

 Target Server Type    : SQLite
 Target Server Version : 3030001
 File Encoding         : 65001

 Date: 11/07/2022 19:11:59
*/

PRAGMA foreign_keys = false;

-- ----------------------------
-- Table structure for maxhigh4_slice
-- ----------------------------
DROP TABLE IF EXISTS "maxhigh4_slice";
CREATE TABLE "maxhigh4_slice" (
  "idx" INT,
  "maxhigh" real
);

PRAGMA foreign_keys = true;
