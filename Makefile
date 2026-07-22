# es-tool Makefile

BINARY      := es-tool
CMD_PATH    := ./cmd/es-tool
BIN_DIR     := bin
BIN         := $(BIN_DIR)/$(BINARY)
GO          ?= go
GOFLAGS     ?=
PKGS        := ./...

# Inject version info (falls back gracefully outside a git checkout).
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := help

## help: Show this help.
.PHONY: help
help:
	@echo "es-tool — available targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'

## build: Build the binary into ./bin.
.PHONY: build
build:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN) $(CMD_PATH)

## install: Install the binary onto your GOPATH/bin.
.PHONY: install
install:
	$(GO) install $(GOFLAGS) -ldflags '$(LDFLAGS)' $(CMD_PATH)

## run: Build and run (pass args via ARGS="ping").
.PHONY: run
run:
	$(GO) run $(CMD_PATH) $(ARGS)

## test: Run all tests.
.PHONY: test
test:
	$(GO) test $(GOFLAGS) $(PKGS)

## test-race: Run all tests with the race detector.
.PHONY: test-race
test-race:
	$(GO) test $(GOFLAGS) -race $(PKGS)

## cover: Run tests and open an HTML coverage report.
.PHONY: cover
cover:
	$(GO) test $(GOFLAGS) -coverprofile=coverage.out $(PKGS)
	$(GO) tool cover -html=coverage.out

## vet: Run go vet.
.PHONY: vet
vet:
	$(GO) vet $(PKGS)

## fmt: Format all Go source.
.PHONY: fmt
fmt:
	$(GO) fmt $(PKGS)

## fmt-check: Fail if any Go source is not gofmt-clean.
.PHONY: fmt-check
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

## tidy: Sync go.mod / go.sum.
.PHONY: tidy
tidy:
	$(GO) mod tidy

## check: Run fmt-check, vet, and tests (CI gate).
.PHONY: check
check: fmt-check vet test

## clean: Remove build artifacts.
.PHONY: clean
clean:
	rm -rf $(BIN_DIR) coverage.out
	$(GO) clean

## all: Clean, check, and build.
.PHONY: all
all: clean check build
