# Slow checks, run nightly (design §11.7, §11.9): the upstream example files,
# and mutation testing of the core packages.
#
# gremlins runs through `go run` at a pinned module version, like govulncheck:
# the Go checksum database verifies it, and it never becomes a dependency of
# the module. Its default timeout is too tight for this suite (every mutant
# "times out"), hence the coefficient.

GREMLINS_VERSION := v0.6.0
MUTATION_PACKAGES ?= preprocess keyfile goption model fix
# Blocking efficacy threshold (killed / (killed + lived)), in percent, for
# every package in MUTATION_PACKAGES (design §11.9). Survivors are reviewed:
# either a missing test, or an equivalent mutant (a capacity hint, a
# redundant bound).
MUTATION_EFFICACY ?= 90
MUTATION_WORKERS ?= 4
# Every test binary runs with its memory capped (tools/mutation/memlimit.sh),
# so a mutant that allocates forever fails instead of exhausting the machine.
# `go run` honours the same -exec: gremlins and the go commands it starts run
# under the cap too, far above what they need.
MUTATION_GOFLAGS := $(strip $(GOFLAGS) -exec=$(CURDIR)/tools/mutation/memlimit.sh)

.PHONY: integration mutation

integration: ## Lint the upstream example files of every embedded tag (network)
	$(GO) test -tags integration -count=1 -run TestUpstreamExamples ./internal/lint/

mutation: ## Mutation testing of the core packages (gremlins, slow)
	@status=0; for p in $(MUTATION_PACKAGES); do \
		echo "== internal/$$p"; \
		GOFLAGS="$(MUTATION_GOFLAGS)" $(GO) run github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION) unleash \
			--timeout-coefficient 20 --workers $(MUTATION_WORKERS) --output-statuses lt \
			--threshold-efficacy $(MUTATION_EFFICACY) ./internal/$$p || status=1; \
	done; exit $$status
