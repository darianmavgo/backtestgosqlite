import argparse
import sqlite3
import pandas as pd
import numpy as np
import random
import os

from deap import base, creator, tools, algorithms

def calculate_features(df):
    """Vectorized calculation of momentum and technical features across all symbols."""
    df = df.sort_values(['symbol', 'date']).copy()
    
    # Per-symbol rolling indicators
    grouped = df.groupby('symbol')
    
    df['mom_5'] = grouped['close'].pct_change(5)
    df['mom_10'] = grouped['close'].pct_change(10)
    df['mom_20'] = grouped['close'].pct_change(20)
    
    df['vol_10'] = grouped['close'].pct_change().groupby(df['symbol']).rolling(10).std().reset_index(0, drop=True)
    
    sma_20 = grouped['close'].rolling(20).mean().reset_index(0, drop=True)
    df['dist_sma20'] = (df['close'] - sma_20) / (sma_20 + 1e-6)
    
    vol_5 = grouped['volume'].rolling(5).mean().reset_index(0, drop=True)
    vol_20 = grouped['volume'].rolling(20).mean().reset_index(0, drop=True)
    df['vol_ratio'] = vol_5 / (vol_20 + 1e-6)
    
    # 5-day forward return target
    df['target_5d'] = grouped['close'].shift(-5) / df['close'] - 1.0
    
    # Replace inf and fill na
    feature_cols = ['mom_5', 'mom_10', 'mom_20', 'vol_10', 'dist_sma20', 'vol_ratio']
    df[feature_cols] = df[feature_cols].replace([np.inf, -np.inf], np.nan).fillna(0.0)
    
    # Cross-sectional z-score standardization per date for well-conditioned weights
    for col in feature_cols:
        mean = df.groupby('date')[col].transform('mean')
        std = df.groupby('date')[col].transform('std')
        df[col] = (df[col] - mean) / (std + 1e-6)
        
    df[feature_cols] = df[feature_cols].replace([np.inf, -np.inf], 0.0).fillna(0.0)
    return df

def setup_deap():
    if "FitnessMax" not in creator.__dict__:
        creator.create("FitnessMax", base.Fitness, weights=(1.0,))
    if "Individual" not in creator.__dict__:
        creator.create("Individual", list, fitness=creator.FitnessMax)

    toolbox = base.Toolbox()
    toolbox.register("attr_float", random.uniform, -1.0, 1.0)
    toolbox.register("individual", tools.initRepeat, creator.Individual, toolbox.attr_float, n=6)
    toolbox.register("population", tools.initRepeat, list, toolbox.individual)

    toolbox.register("mate", tools.cxBlend, alpha=0.5)
    toolbox.register("mutate", tools.mutGaussian, mu=0, sigma=0.2, indpb=0.25)
    toolbox.register("select", tools.selTournament, tournsize=3)
    return toolbox

def evolve_weights(toolbox, feat_matrix, target_matrix):
    """
    feat_matrix: (N_weeks, N_stocks, 6)
    target_matrix: (N_weeks, N_stocks)
    """
    n_weeks, n_stocks, n_feats = feat_matrix.shape
    if n_weeks == 0:
        return [1.0, 0.5, 0.2, 0.1, 0.5, 0.2] # Fallback default momentum weights

    feat_clean = np.nan_to_num(feat_matrix, nan=0.0)
    target_clean = np.nan_to_num(target_matrix, nan=0.0)

    def evaluate(ind):
        w = np.array(ind, dtype=np.float32)
        # scores: (n_weeks, n_stocks)
        scores = np.tensordot(feat_clean, w, axes=([2], [0]))
        # Pick top stock for each training week
        top_stocks = np.argmax(scores, axis=1)
        chosen_returns = target_clean[np.arange(n_weeks), top_stocks]
        
        # Fitness: mean return - 0.2 * std (Sharpe-like risk adjusted return)
        mean_ret = float(np.mean(chosen_returns))
        std_ret = float(np.std(chosen_returns))
        score = mean_ret - 0.2 * std_ret
        return (score,)

    toolbox.register("evaluate", evaluate)
    pop = toolbox.population(n=20)
    hof = tools.HallOfFame(1)
    algorithms.eaSimple(pop, toolbox, cxpb=0.6, mutpb=0.25, ngen=10, halloffame=hof, verbose=False)
    return list(hof[0])

def main():
    parser = argparse.ArgumentParser(description="DEAP Genetic Momentum Predictor")
    parser.add_argument("--market-db", required=True, help="Path to market history SQLite database")
    parser.add_argument("--calc-db", required=True, help="Path to output strategy SQLite database")
    parser.add_argument("--symbols", required=True, help="Comma-separated symbols list")
    parser.add_argument("--date", default="", help="Specific date to predict (optional)")
    parser.add_argument("--mode", default="weekly", choices=["weekly", "single"], help="Execution mode")
    parser.add_argument("--table", default="backtest_start", help="Market history table name")
    args = parser.parse_args()

    symbols = [s.strip().upper() for s in args.symbols.split(",") if s.strip()]
    if not symbols:
        print("No symbols provided.")
        return

    # Ensure parent directory of calc_db exists
    calc_dir = os.path.dirname(args.calc_db)
    if calc_dir:
        os.makedirs(calc_dir, exist_ok=True)

    # 1. Load Data from SQLite
    conn = sqlite3.connect(args.market_db)
    placeholders = ",".join("?" for _ in symbols)
    query = f"""
    SELECT symbol, Date as date, open, high, low, close, volume
    FROM {args.table}
    WHERE symbol IN ({placeholders})
    ORDER BY symbol, date ASC
    """
    df = pd.read_sql_query(query, conn, params=symbols)
    conn.close()

    if len(df) == 0:
        print("No market data found.")
        return

    # 2. Extract Features
    df = calculate_features(df)
    feature_cols = ['mom_5', 'mom_10', 'mom_20', 'vol_10', 'dist_sma20', 'vol_ratio']

    # 3. Identify Weekly Calendar Cycles
    df['dt'] = pd.to_datetime(df['date'])
    df['year_week'] = df['dt'].dt.isocalendar().year.astype(str) + "_" + df['dt'].dt.isocalendar().week.astype(str).str.zfill(2)

    # Build weekly schedule: for each week, find first trading day, last trading day, and day count
    weekly_meta = df.groupby('year_week').agg(
        first_date=('date', 'min'),
        last_date=('date', 'max'),
        trading_days=('date', 'nunique')
    ).reset_index().sort_values('first_date').reset_index(drop=True)

    # Filter out initial warmup weeks (need at least 25 trading days of history)
    all_dates = sorted(df['date'].unique())
    if len(all_dates) < 30:
        print("Not enough history for feature warmup.")
        return
    warmup_date = all_dates[25]
    weekly_meta = weekly_meta[weekly_meta['first_date'] >= warmup_date].reset_index(drop=True)

    toolbox = setup_deap()
    current_weights = [1.0, 0.5, 0.2, 0.1, 0.5, 0.2]
    
    out_conn = sqlite3.connect(args.calc_db)
    cursor = out_conn.cursor()
    cursor.execute("""
    CREATE TABLE IF NOT EXISTS predictions (
        date TEXT PRIMARY KEY,
        end_date TEXT,
        trading_days INTEGER,
        predicted_symbol TEXT,
        probability REAL,
        momentum_score REAL
    )
    """)
    cursor.execute("""
    CREATE TABLE IF NOT EXISTS weekly_rankings (
        date TEXT,
        symbol TEXT,
        rank INTEGER,
        probability REAL,
        momentum_score REAL,
        PRIMARY KEY (date, symbol)
    )
    """)

    print(f"🧠 DEAP Genetic Momentum: Scanning {len(symbols)} stocks across {len(weekly_meta)} trading weeks...")
    
    # Pre-aggregate date lookup
    date_symbol_data = {}
    for (d, sym), row in df.set_index(['date', 'symbol']).iterrows():
        if d not in date_symbol_data:
            date_symbol_data[d] = {}
        date_symbol_data[d][sym] = (row[feature_cols].values.astype(np.float32), float(row['target_5d']) if pd.notnull(row['target_5d']) else 0.0)

    predictions_batch = []
    rankings_batch = []
    
    # Retrain every 4 weeks using rolling window of past 16 weeks
    train_history_weeks = []
    
    for idx, w_row in weekly_meta.iterrows():
        first_d = w_row['first_date']
        last_d = w_row['last_date']
        tdays = int(w_row['trading_days'])
        
        # If single date requested and doesn't match, skip
        if args.date and first_d != args.date:
            if first_d < args.date and first_d in date_symbol_data:
                train_history_weeks.append(first_d)
            continue

        # Periodically evolve DEAP weights every 4 weeks
        if idx % 4 == 0 and len(train_history_weeks) >= 4:
            recent_weeks = train_history_weeks[-16:]
            feats_list = []
            targets_list = []
            
            valid_weeks_count = 0
            for tw in recent_weeks:
                tw_data = date_symbol_data.get(tw, {})
                tw_syms = [s for s in symbols if s in tw_data]
                if len(tw_syms) < 20:
                    continue
                
                week_f = np.nan_to_num(np.array([tw_data[s][0] for s in tw_syms]), nan=0.0)
                week_t = np.nan_to_num(np.array([tw_data[s][1] for s in tw_syms]), nan=0.0)
                feats_list.append(week_f)
                targets_list.append(week_t)
                valid_weeks_count += 1
                
            if valid_weeks_count >= 4:
                min_s = min(len(f) for f in feats_list)
                f_mat = np.array([f[:min_s] for f in feats_list])
                t_mat = np.array([t[:min_s] for t in targets_list])
                current_weights = evolve_weights(toolbox, f_mat, t_mat)

        # Score all 50 symbols for the current week's first date
        cur_data = date_symbol_data.get(first_d, {})
        available_syms = [s for s in symbols if s in cur_data]
        if not available_syms:
            continue
            
        cur_feats = np.nan_to_num(np.array([cur_data[s][0] for s in available_syms]), nan=0.0) # (N, 6)
        w_arr = np.array(current_weights, dtype=np.float32)
        raw_scores = np.nan_to_num(np.dot(cur_feats, w_arr), nan=-999.0) # (N,)
        
        # Compute Softmax Probability
        std_val = float(np.std(raw_scores))
        tau = max(std_val, 0.1) if not np.isnan(std_val) else 1.0
        shifted_scores = (raw_scores - np.max(raw_scores)) / tau
        exp_scores = np.exp(shifted_scores)
        sum_exp = np.sum(exp_scores)
        if sum_exp > 0:
            probabilities = exp_scores / sum_exp
        else:
            probabilities = np.ones_like(exp_scores) / len(exp_scores)
        
        # Rank symbols by probability descending
        sorted_indices = np.argsort(-probabilities)
        top_idx = sorted_indices[0]
        best_sym = available_syms[top_idx]
        best_prob = float(probabilities[top_idx])
        best_score = float(raw_scores[top_idx])
        
        predictions_batch.append((first_d, last_d, tdays, best_sym, best_prob, best_score))
        
        for rank_pos, sym_idx in enumerate(sorted_indices, start=1):
            rankings_batch.append((
                first_d,
                available_syms[sym_idx],
                rank_pos,
                float(probabilities[sym_idx]),
                float(raw_scores[sym_idx])
            ))
            
        train_history_weeks.append(first_d)

    # 4. Insert into SQLite
    cursor.executemany("""
    INSERT OR REPLACE INTO predictions (date, end_date, trading_days, predicted_symbol, probability, momentum_score)
    VALUES (?, ?, ?, ?, ?, ?)
    """, predictions_batch)

    cursor.executemany("""
    INSERT OR REPLACE INTO weekly_rankings (date, symbol, rank, probability, momentum_score)
    VALUES (?, ?, ?, ?, ?)
    """, rankings_batch)

    out_conn.commit()
    out_conn.close()
    print(f"✅ Successfully stored {len(predictions_batch)} weekly predictions and {len(rankings_batch)} rankings in {args.calc_db}")

if __name__ == "__main__":
    main()
