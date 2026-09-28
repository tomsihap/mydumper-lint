# mydumper-lint developer entry points. Run `make help` for the list.
# Tools that are not part of the Go toolchain run from pinned Docker images,
# so contributors only need Go and Docker.

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
GO ?= go
BIN := bin/mydumper-lint
PKGS := ./...

.DEFAULT_GOAL := help

.PHONY: cover-check all build test test-short cover vet fmt tidy clean help

all: build test ## Build and run the unit tests

build: ## Build the static binary into bin/
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN) ./cmd/mydumper-lint

test: ## Run unit and golden tests with the race detector
	$(GO) test -race -count=1 $(PKGS)

test-short: ## Run unit tests without the race detector (fast)
	$(GO) test -count=1 $(PKGS)

cover: ## Run tests with coverage and print the per-package summary
	$(GO) test -count=1 -coverprofile=coverage.out $(PKGS)
	$(GO) tool cover -func=coverage.out | tail -n 1

cover-check: cover ## Enforce the coverage thresholds of design §11.9
	$(GO) run ./tools/covercheck coverage.out

vet: ## Run go vet
	$(GO) vet $(PKGS)

fmt: ## Format Go code
	$(GO) fmt $(PKGS)

tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

clean: ## Remove build and test outputs
	rm -rf bin dist coverage.out

# Feature-specific targets live in mk/*.mk (oracle, lint, e2e, release, …).
-include $(sort $(wildcard mk/*.mk))

help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_.-]+:.*?## ' $(MAKEFILE_LIST) | sort -u \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'
