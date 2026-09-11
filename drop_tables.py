import sqlite3

conn = sqlite3.connect('data/market_history.db')
cursor = conn.cursor()
cursor.execute("SELECT name FROM sqlite_master WHERE type='table';")
tables = cursor.fetchall()
for table in tables:
    table_name = table[0]
    if table_name != 'backtest_start' and table_name != 'sqlite_sequence':
        cursor.execute(f"DROP TABLE IF EXISTS {table_name};")
        print(f"Dropped {table_name}")
conn.commit()
conn.execute("VACUUM;")
conn.close()
