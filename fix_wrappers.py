import glob, re

files = glob.glob("pkg/strategy/*.go")
for f in files:
    if f.endswith("_test.go") or "sql_strategy" in f or "voo_tecl" in f or "strategy.go" in f or "registry.go" in f or "params.go" in f or "indicators.go" in f or "buy_and_hold.go" in f:
        continue
    with open(f, "r") as file:
        content = file.read()
    
    # 1. replace type X struct{} with fields
    content = re.sub(r'type ([A-Za-z0-9_]+) struct\{\}', r'type \1 struct {\n\tmarketDBPath string\n\tcalcDBPath   string\n}', content)
    
    # 2. replace the empty SetDatabases
    content = re.sub(
        r'func \(s \*([A-Za-z0-9_]+)\) SetDatabases\(marketDBPath, calcDBPath string\) \{\}',
        r'func (s *\1) SetDatabases(marketDBPath, calcDBPath string) {\n\ts.marketDBPath = marketDBPath\n\ts.calcDBPath = calcDBPath\n}',
        content
    )
    
    # 3. remove "data/market_history.db", 
    content = content.replace('"data/market_history.db", ', '')
    
    # 4. add pipe.SetDatabases before return pipe.GenerateSignals
    content = re.sub(r'(pipe := NewSQLPipelineStrategy.*?)\n(\s*return pipe\.GenerateSignals)', r'\1\n\tpipe.SetDatabases(s.marketDBPath, s.calcDBPath)\n\2', content)
    
    with open(f, "w") as file:
        file.write(content)
        print(f"Fixed {f}")
