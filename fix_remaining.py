import re

for f in ["pkg/strategy/wc_4day_hold.go", "pkg/strategy/whitings_creek.go"]:
    with open(f, "r") as file:
        content = file.read()
    content = content.replace('"data/market_history.db", ', '')
    with open(f, "w") as file:
        file.write(content)
