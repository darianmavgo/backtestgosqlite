import glob, re

files = glob.glob("sql/strategies/**/*.sql", recursive=True)
for f in files:
    with open(f, "r") as file:
        content = file.read()
    
    # Remove any CREATE INDEX lines involving backtest_start
    content = re.sub(r'(?m)^.*CREATE.*INDEX.*backtest_start.*$', '', content)
    
    with open(f, "w") as file:
        file.write(content)
