# Static analysis: golangci-lint, govulncheck and actionlint (design §11.9, §12.1).
#
# Every tool is pinned. golangci-lint and actionlint run from their official
# Docker images, pinned by tag and digest, so contributors only need Go and
# Docker. govulncheck runs through `go run` at a pinned module version, which the
# Go checksum database verifies.
#
# GOLANGCI_LINT_VERSION must equal the version pinned in .github/workflows/ci.yml;
# the CI lint job fails when they differ. Bump an image's tag and digest together:
#   docker buildx imagetools inspect IMAGE:TAG --format '{{json .Manifest.Digest}}'
#
# Gate on exit codes only, never on an issue count: a run that fails part-way
# reports fewer issues, not more.

GOLANGCI_LINT_VERSION := v2.13.2
GOLANGCI_LINT_IMAGE := golangci/golangci-lint:$(GOLANGCI_LINT_VERSION)@sha256:ba07dffad130794ae79ebaa0056809d18c0168f3f846480ffd3eb6c04578b83d
GOVULNCHECK_VERSION := v1.8.0
ACTIONLINT_IMAGE := rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667

DOCKER ?= docker
# Docker volume that keeps the Go build cache, the module cache and
# golangci-lint's own cache between runs (`docker volume rm` it to start cold).
LINT_CACHE_VOLUME ?= mydumper-lint-golangci-cache

# The container runs as the image's default user: a fresh named volume is only
# writable by root, and fixes are written in place, so files keep their owner.
GOLANGCI_LINT = $(DOCKER) run --rm \
	--volume "$(CURDIR):/src" --workdir /src \
	--volume $(LINT_CACHE_VOLUME):/cache \
	--env GOCACHE=/cache/go-build \
	--env GOMODCACHE=/cache/mod \
	--env GOLANGCI_LINT_CACHE=/cache/golangci-lint \
	$(GOLANGCI_LINT_IMAGE) golangci-lint

.PHONY: lint lint-fix vuln actionlint check-all golangci-lint-version

# `config verify` downloads golangci-lint's JSON schema, so it needs the network.
# It is the only check that catches keys golangci-lint v2 silently ignores.
lint: ## Lint Go code with golangci-lint (verifies .golangci.yml first)
	$(GOLANGCI_LINT) config verify
	$(GOLANGCI_LINT) run

lint-fix: ## Apply golangci-lint fixes and formatting (gofumpt, goimports)
	$(GOLANGCI_LINT) run --fix

vuln: ## Scan dependencies and the standard library with govulncheck
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) $(PKGS)

actionlint: ## Lint the GitHub Actions workflows (actionlint with shellcheck)
	$(DOCKER) run --rm --volume "$(CURDIR):/repo" --workdir /repo $(ACTIONLINT_IMAGE) -color

check-all: vet lint vuln test ## Run go vet, golangci-lint, govulncheck and the tests

# Prints the pinned version; the CI lint job compares it with its own pin.
golangci-lint-version:
	@echo $(GOLANGCI_LINT_VERSION)
