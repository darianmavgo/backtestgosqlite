#!/usr/bin/env python3

import yfinance as yf
import pandas as pd
import sqlalchemy
import sqlite3

con = sqlite3.connect('/Users/darianhickman/Documents/wc_2022/settings.db')
cur = con.cursor()
config = cur.execute(
    'select loadtablepath, symbol_list, start_date, end_date, exclude_list from config limit 1;')
c = config.fetchone()
print(c)
engine = sqlalchemy.create_engine('sqlite:///'+c[0], echo=False)

for row in cur.execute('SELECT symbol FROM '+c[1]+' where symbol not in (select distinct symbol from '+c[4]+');'):
    symbol = row[0]
    print(symbol)
    data = yf.download(symbol, start=c[2], end=c[3])
    data['symbol'] = symbol
    data.to_sql(name="backtest_start", if_exists="append", con=engine)

