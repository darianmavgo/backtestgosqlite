import re

with open("pkg/runner/runner.go", "r") as f:
    content = f.read()

# Add sort import if not present
if '"sort"' not in content:
    content = content.replace('import (', 'import (\n\t"sort"', 1)

# Replace PrintComparisonTable
old_func = r'func PrintComparisonTable\(results \[\]RunResult\) \{'
new_func = r'''func PrintComparisonTable(results []RunResult) {
	// Sort by CAGR descending
	sort.Slice(results, func(i, j int) bool {
		if results[i].Err != nil { return false }
		if results[j].Err != nil { return true }
		return results[i].Report.CAGR > results[j].Report.CAGR
	})'''

content = re.sub(old_func, new_func, content)

with open("pkg/runner/runner.go", "w") as f:
    f.write(content)
