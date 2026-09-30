# The browser playground (design §14, M6): mydumper-lint compiled to
# WebAssembly, and a static page around it (web/playground). The build output
# (mydumper-lint.wasm, and wasm_exec.js, which must come from the same Go
# release) is not committed; .github/workflows/pages.yml publishes it.

PLAYGROUND_DIR := web/playground
PLAYGROUND_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: playground playground-test playground-serve

playground: ## Build the WebAssembly playground into web/playground
	GOOS=js GOARCH=wasm $(GO) build -trimpath \
		-ldflags "-s -w -X github.com/tomsihap/mydumper-lint/internal/buildinfo.Version=$(PLAYGROUND_VERSION)" \
		-o $(PLAYGROUND_DIR)/mydumper-lint.wasm ./cmd/mydumper-lint-wasm
	cp "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" $(PLAYGROUND_DIR)/wasm_exec.js

playground-test: playground ## Test the WebAssembly build: its examples (Node) and internal/playground under js/wasm
	node $(PLAYGROUND_DIR)/examples.test.mjs
	PATH="$$($(GO) env GOROOT)/lib/wasm:$$PATH" GOOS=js GOARCH=wasm $(GO) test ./internal/playground

playground-serve: playground ## Serve the playground on http://localhost:8765
	python3 -m http.server 8765 --bind 127.0.0.1 --directory $(PLAYGROUND_DIR)
