.PHONY: test coverage lint build agentctl install clean

GO ?= go
PKG ?= ./...
CMD ?= ./cmd/agentctl
BIN_DIR ?= bin
BINARY ?= $(BIN_DIR)/agentctl
COVERAGE_FILE ?= coverage.out

test:
	$(GO) test $(PKG)

coverage:
	$(GO) test -covermode=atomic -coverprofile=$(COVERAGE_FILE) $(PKG)
	$(GO) tool cover -func=$(COVERAGE_FILE)

lint:
	golangci-lint run

build: agentctl

agentctl:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY) $(CMD)

install:
	$(GO) install $(CMD)

clean:
	rm -rf $(BIN_DIR) $(COVERAGE_FILE)
