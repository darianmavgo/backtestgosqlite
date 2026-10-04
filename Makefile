.PHONY: all build clean test tidy list

GOCMD=go
BIN_DIR=bin
CMDS := $(notdir $(wildcard cmd/*))

all: build

build:
	@mkdir -p $(BIN_DIR)
	@for c in $(CMDS); do \
		echo "Building cmd/$$c..."; \
		$(GOCMD) build -o $(BIN_DIR)/$$c ./cmd/$$c || exit 1; \
	done
	@echo "Binaries are in $(BIN_DIR)/"

tidy:
	$(GOCMD) mod tidy

test:
	$(GOCMD) test ./pkg/... ./cmd/...

list: build
	./$(BIN_DIR)/backtest -strategylist

clean:
	rm -rf $(BIN_DIR)/*
