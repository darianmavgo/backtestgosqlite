import re

with open("pkg/runner/runner.go", "r") as f:
    content = f.read()

# 1. Change package
content = content.replace("package main", "package runner")

# 2. Delete func main()
# Find start of func main()
main_idx = content.find("func main() {")
if main_idx != -1:
    # Find matching brace
    brace_count = 0
    end_idx = -1
    for i in range(main_idx, len(content)):
        if content[i] == '{':
            brace_count += 1
        elif content[i] == '}':
            brace_count -= 1
            if brace_count == 0:
                end_idx = i
                break
    if end_idx != -1:
        content = content[:main_idx] + content[end_idx+1:]

# 3. Capitalize types and functions
replacements = {
    "type runResult": "type RunResult",
    "func executeStrategy": "func ExecuteStrategy",
    "func detectAndDownloadMissingData": "func DetectAndDownloadMissingData",
    "func buildConfig": "func BuildConfig",
    "func printComparisonTable": "func PrintComparisonTable",
    "func printTradesTable": "func PrintTradesTable",
    "func printPerformanceTearSheet": "func PrintPerformanceTearSheet",
    "func runDownload": "func RunDownload",
    "[]runResult": "[]RunResult",
    "runResult{": "RunResult{",
}
for old, new in replacements.items():
    content = content.replace(old, new)

# 4. Capitalize RunResult fields
# type RunResult struct {
# 	strat       strategy.Strategy
# 	report      models.PerformanceReport
# 	trades      []models.Trade
# 	equityCurve []models.DailyEquityPoint
# 	dbPath      string
# 	signalCount int
# 	err         error
# }
struct_pattern = r'type RunResult struct \{[^\}]+\}'
def fix_struct(m):
    s = m.group(0)
    s = s.replace("strat       ", "Strat       ")
    s = s.replace("report      ", "Report      ")
    s = s.replace("trades      ", "Trades      ")
    s = s.replace("equityCurve ", "EquityCurve ")
    s = s.replace("dbPath      ", "DbPath      ")
    s = s.replace("signalCount ", "SignalCount ")
    s = s.replace("err         ", "Err         ")
    return s
content = re.sub(struct_pattern, fix_struct, content)

field_replacements = [
    (r'\.strat', r'.Strat'),
    (r'\.report', r'.Report'),
    (r'\.trades', r'.Trades'),
    (r'\.equityCurve', r'.EquityCurve'),
    (r'\.dbPath', r'.DbPath'),
    (r'\.signalCount', r'.SignalCount'),
    (r'\.err', r'.Err'),
    (r'strat:\s', r'Strat: '),
    (r'report:\s', r'Report: '),
    (r'trades:\s', r'Trades: '),
    (r'equityCurve:\s', r'EquityCurve: '),
    (r'dbPath:\s', r'DbPath: '),
    (r'signalCount:\s', r'SignalCount: '),
    (r'err:\s', r'Err: '),
]
for old, new in field_replacements:
    content = re.sub(old, new, content)

with open("pkg/runner/runner.go", "w") as f:
    f.write(content)
