import sys
import sqlite3
import pandas as pd
import numpy as np
from statsmodels.tsa.stattools import grangercausalitytests

def main():
    if len(sys.argv) < 2:
        print("Usage: python voo_gld_uten_stats.py <path_to_db>")
        sys.exit(1)

    db_path = sys.argv[1]
    print(f"[Python] Connecting to {db_path}...")

    conn = sqlite3.connect(db_path)
    
    # Read the aligned data
    query = "SELECT * FROM prototyping ORDER BY date ASC"
    df = pd.read_sql(query, conn)
    
    if df.empty:
        print("[Python] Error: prototyping table is empty.")
        sys.exit(1)

    print(f"[Python] Loaded {len(df)} rows. Computing Granger Causality...")

    # We will test if VOO returns Granger-cause GLD and UTEN returns
    # and vice versa. Max lag = 10 minutes.
    
    results_gc = []
    maxlag = 10

    # VOO -> GLD
    try:
        # data needs to be 2D array: [target, predictor]
        # testing if VOO causes GLD => target=GLD, predictor=VOO
        res = grangercausalitytests(df[['gld_return', 'voo_return']].dropna(), maxlag=maxlag, verbose=False)
        for lag, test in res.items():
            p_value = test[0]['ssr_ftest'][1]
            results_gc.append(('VOO', 'GLD', lag, p_value))
    except Exception as e:
        print(f"[Python] Error computing VOO->GLD granger: {e}")

    # VOO -> UTEN
    try:
        res = grangercausalitytests(df[['uten_return', 'voo_return']].dropna(), maxlag=maxlag, verbose=False)
        for lag, test in res.items():
            p_value = test[0]['ssr_ftest'][1]
            results_gc.append(('VOO', 'UTEN', lag, p_value))
    except Exception as e:
        print(f"[Python] Error computing VOO->UTEN granger: {e}")

    # Write Granger Causality to DB
    df_gc = pd.DataFrame(results_gc, columns=['predictor', 'target', 'lag_minutes', 'p_value'])
    df_gc.to_sql('granger_causality', conn, if_exists='replace', index=False)
    print(f"[Python] Wrote granger_causality table.")

    print(f"[Python] Computing Volatility Spillover (15m rolling variance)...")
    
    # Calculate 15-period rolling variance
    df['voo_var_15m'] = df['voo_return'].rolling(window=15).var()
    df['gld_var_15m'] = df['gld_return'].rolling(window=15).var()
    df['uten_var_15m'] = df['uten_return'].rolling(window=15).var()
    
    # We want to see cross-correlation of volatility.
    df_vol = df[['date', 'voo_var_15m', 'gld_var_15m', 'uten_var_15m']].dropna()
    
    # Save the rolling variances back so the user can query them
    df_vol.to_sql('volatility_spillover', conn, if_exists='replace', index=False)
    
    # Print the overall correlation matrix
    print("[Python] Volatility Correlation Matrix (15m):")
    print(df_vol[['voo_var_15m', 'gld_var_15m', 'uten_var_15m']].corr())

    print("[Python] Script completed successfully.")
    conn.close()

if __name__ == "__main__":
    main()
