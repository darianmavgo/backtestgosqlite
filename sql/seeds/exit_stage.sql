DROP TABLE IF EXISTS exit_stage;
CREATE TABLE exit_stage AS
    SELECT p.symbol, p.qty as qty, t.time_up, t.exit_date FROM time_up as t JOIN position as p ON p.symbol = t.symbol
    WHERE time_up = 1;