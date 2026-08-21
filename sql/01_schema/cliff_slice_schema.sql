/*
 Navicat Premium Data Transfer

 Source Server         : ALTG2020-09-21
 Source Server Type    : SQLite
 Source Server Version : 3030001
 Source Schema         : main

 Target Server Type    : SQLite
 Target Server Version : 3030001
 File Encoding         : 65001

 Date: 21/09/2020 17:45:19
*/

PRAGMA foreign_keys = false;

-- ----------------------------
-- Table structure for cliff_slice
-- ----------------------------
DROP TABLE IF EXISTS "cliff_slice";
CREATE TABLE cliff_slice (
	level_0 BIGINT, 
	"index" BIGINT, 
	symbol TEXT, 
	close FLOAT, 
	minlow FLOAT, 
	foundcliff BOOLEAN, 
	CHECK (foundcliff IN (0, 1))
);

-- ----------------------------
-- Indexes structure for table cliff_slice
-- ----------------------------
CREATE INDEX "main"."ix_cliff_slice_level_0"
ON "cliff_slice" (
  "level_0" ASC
);

PRAGMA foreign_keys = true;
