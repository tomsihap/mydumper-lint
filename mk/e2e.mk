# End-to-end suite (design §11.6, e2e/README.md): the official mydumper images
# against a seeded MySQL 8.4 server, driven by `go test -tags e2e ./e2e/...`.
# Needs Docker with the compose plugin.
#
# E2E_VERSIONS selects the mydumper versions: empty for the default matrix (the
# latest stable image-verified release of each branch), `all`, or a
# comma-separated list of tags. E2E_PARALLEL bounds the concurrent containers;
# E2E_FLAGS adds go test flags, e.g. E2E_FLAGS='-run TestScenarios/mdl102 -v'.
# Results: e2e/.out/summary.md, one log per run in e2e/.out/logs/.

E2E_COMPOSE := docker compose -f e2e/compose.yaml
E2E_PARALLEL ?= 4
E2E_GO_TIMEOUT ?= 90m
E2E_FLAGS ?=
E2E_SUMMARY := e2e/.out/summary.md

.PHONY: e2e-up e2e e2e-all e2e-down

e2e-up: ## Start the e2e MySQL service and wait until it is healthy
	$(E2E_COMPOSE) up --detach --wait

# E2E_VERSIONS reaches go test through the environment (make exports variables
# that come from the environment or the command line); it is never expanded
# into the recipe. The summary is printed whatever the outcome; the exit
# status is go test's.
e2e: e2e-up ## Run the end-to-end suite on the default version matrix (Docker)
	@rm -f $(E2E_SUMMARY)
	@status=0; \
	$(GO) test -tags e2e -count=1 -timeout $(E2E_GO_TIMEOUT) \
		-parallel $(E2E_PARALLEL) $(E2E_FLAGS) ./e2e/... || status=$$?; \
	if [[ -f $(E2E_SUMMARY) ]]; then cat $(E2E_SUMMARY); fi; \
	exit $$status

e2e-all: ## Run the end-to-end suite on every image-verified mydumper version
	$(MAKE) --no-print-directory e2e E2E_VERSIONS=all E2E_GO_TIMEOUT=240m

e2e-down: ## Stop the e2e MySQL service and remove leftover tool containers
	@ids=$$(docker ps --all --quiet --filter label=mydumper-lint-e2e); \
	if [[ -n "$$ids" ]]; then docker rm --force $$ids; fi
	$(E2E_COMPOSE) down --volumes --remove-orphans
