GO ?= go
NODE ?= node
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= v2.15.0

BINARY_NAME ?= woovi-pix-tester
BIN_DIR ?= bin
DIST_DIR ?= dist
TARGET_OS ?= $(shell $(GO) env GOOS)
TARGET_ARCH ?= $(shell $(GO) env GOARCH)

GO_FILES := $(shell find . -type f -name '*.go' -not -path './.git/*' -not -path './vendor/*')
LINT_BIN := $(BIN_DIR)/golangci-lint
RELEASE_NAME := $(BINARY_NAME)-$(TARGET_OS)-$(TARGET_ARCH)
RELEASE_DIR := $(DIST_DIR)/$(RELEASE_NAME)
RELEASE_ARCHIVE := $(RELEASE_DIR).tar.gz

ifeq ($(TARGET_OS),windows)
EXE_SUFFIX := .exe
else
EXE_SUFFIX :=
endif

.PHONY: help dev run fmt fmt-check test test-race vet lint-install lint js-check check build package coverage

help: ## Show available development, validation, and packaging targets
	@printf '%s\n' \
		'Development:' \
		'  make dev                         Run the local server (WOOVI_APP_ID required)' \
		'  make run                         Alias for make dev' \
		'' \
		'Validation:' \
		'  make fmt                         Format Go files' \
		'  make fmt-check                   Check Go formatting without changes' \
		'  make test                        Run uncached Go tests' \
		'  make test-race                   Run Go tests with the race detector' \
		'  make vet                         Run go vet' \
		'  make lint-install                Install the pinned golangci-lint into bin/' \
		'  make lint                        Run golangci-lint' \
		'  make js-check                    Check browser JavaScript syntax (Node.js required)' \
		'  make check                       Run formatting, lint, vet, tests, JS check, and build' \
		'  make coverage                    Show Go test coverage' \
		'' \
		'Build and deployment artifact:' \
		'  make build                       Build a binary for TARGET_OS/TARGET_ARCH' \
		'  make package                     Create a tar.gz artifact in dist/' \
		'  make package TARGET_OS=linux TARGET_ARCH=amd64'

dev: ## Start the local server; provide WOOVI_APP_ID in the environment
	@test -n "$$WOOVI_APP_ID" || { printf '%s\n' 'WOOVI_APP_ID is required; set a sandbox AppID in your environment.' >&2; exit 1; }
	$(GO) run .

run: dev ## Alias for dev

fmt: ## Format all Go source and test files
	gofmt -w $(GO_FILES)

fmt-check: ## Check Go formatting without changing files
	@unformatted="$$(gofmt -l $(GO_FILES))"; \
	if [ -n "$$unformatted" ]; then \
		printf 'Go files need formatting:\n%s\n' "$$unformatted"; \
		exit 1; \
	fi

test: ## Run the full Go test suite without cached results
	$(GO) test -count=1 ./...

test-race: ## Run the full Go test suite with the race detector
	$(GO) test -race -count=1 ./...

vet: ## Run Go static analysis
	$(GO) vet ./...

lint-install: ## Install the pinned golangci-lint into the ignored bin/ directory
	@mkdir -p "$(BIN_DIR)"
	GOBIN="$(CURDIR)/$(BIN_DIR)" $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: ## Run golangci-lint from PATH or bin/
	@if command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1; then \
		"$(GOLANGCI_LINT)" run ./...; \
	elif [ -x "$(LINT_BIN)" ]; then \
		"$(LINT_BIN)" run ./...; \
	else \
		printf '%s\n' 'golangci-lint is missing; run make lint-install or set GOLANGCI_LINT.' >&2; \
		exit 127; \
	fi

js-check: ## Check the browser JavaScript syntax
	$(NODE) --check static/app.js

check: fmt-check lint vet test js-check build ## Run the complete local validation gate

build: ## Build a binary for TARGET_OS/TARGET_ARCH into bin/
	@mkdir -p "$(BIN_DIR)"
	CGO_ENABLED=0 GOOS="$(TARGET_OS)" GOARCH="$(TARGET_ARCH)" $(GO) build -trimpath -o "$(BIN_DIR)/$(RELEASE_NAME)$(EXE_SUFFIX)" .

package: ## Build a portable deployment archive for TARGET_OS/TARGET_ARCH
	@mkdir -p "$(RELEASE_DIR)"
	CGO_ENABLED=0 GOOS="$(TARGET_OS)" GOARCH="$(TARGET_ARCH)" $(GO) build -trimpath -ldflags="-s -w" -o "$(RELEASE_DIR)/$(BINARY_NAME)$(EXE_SUFFIX)" .
	cp README.md "$(RELEASE_DIR)/README.md"
	tar -czf "$(RELEASE_ARCHIVE)" -C "$(RELEASE_DIR)" "$(BINARY_NAME)$(EXE_SUFFIX)" README.md
	@printf 'Deployment artifact: %s\n' "$(RELEASE_ARCHIVE)"

coverage: ## Run tests and print Go coverage by function
	@mkdir -p "$(BIN_DIR)"
	$(GO) test -count=1 -coverprofile="$(BIN_DIR)/coverage.out" ./...
	$(GO) tool cover -func="$(BIN_DIR)/coverage.out"
