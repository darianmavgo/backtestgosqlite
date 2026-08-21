/*
 Navicat Premium Data Transfer

 Source Server         : exits.sqlite
 Source Server Type    : SQLite
 Source Server Version : 3030001
 Source Schema         : main

 Target Server Type    : SQLite
 Target Server Version : 3030001
 File Encoding         : 65001

 Date: 11/08/2022 01:15:48
*/

PRAGMA foreign_keys = false;

-- ----------------------------
-- Table structure for order
-- ----------------------------
DROP TABLE IF EXISTS "order";
CREATE TABLE `order` (`id` TEXT NULL, `client_order_id` TEXT NULL, `created_at` DATETIME NULL, `updated_at` DATETIME NULL, `submitted_at` DATETIME NULL, `filled_at` DATETIME NULL, `expired_at` DATETIME NULL, `canceled_at` DATETIME NULL, `failed_at` DATETIME NULL, `replaced_at` DATETIME NULL, `replaces` TEXT NULL, `replaced_by` TEXT NULL, `asset_id` TEXT NULL, `symbol` TEXT NULL, `exchange` TEXT NULL, `class` TEXT NULL, `qty` TEXT NULL, `notional` TEXT NULL, `filled_qty` TEXT NULL, `type` TEXT NULL, `side` TEXT NULL, `time_in_force` TEXT NULL, `limit_price` TEXT NULL, `filled_avg_price` TEXT NULL, `stop_price` TEXT NULL, `trail_price` TEXT NULL, `trail_percent` TEXT NULL, `hwm` TEXT NULL, `status` TEXT NULL, `extended_hours` INTEGER NULL, `legs` TEXT NULL);

PRAGMA foreign_keys = true;
