# GLib oracle (tools/oracle): a test-only C program, GPL-3.0-or-later, never
# linked into the mydumper-lint binary. See tools/oracle/README.md.

ORACLE_BIN := bin/oracle
ORACLE_SRC := tools/oracle/oracle.c
ORACLE_CFLAGS ?= -O2 -Wall -Wextra
ORACLE_IMAGE ?= mydumper-lint-oracle
# One image per GLib version: alma9 (2.68, official mydumper images),
# noble (2.80, distribution packages), edge (newest GLib).
ORACLE_FLAVORS ?= alma9 noble edge
# The repository is mounted read-only as the working directory, so the
# relative case paths the runner passes resolve inside the container.
ORACLE_DOCKER_RUN = docker run --rm -i -v "$(CURDIR):/w:ro" -w /w

# Differential runs: the emulators against real GLib, on every image.
ORACLE_FUZZTIME ?= 60s
GOPTION_CASES ?= 20000

.PHONY: oracle oracle-images oracle-conformance oracle-diff

oracle: $(ORACLE_BIN) ## Build the GLib oracle into bin/ (needs cc, pkg-config and GLib)

$(ORACLE_BIN): $(ORACLE_SRC)
	@mkdir -p $(@D)
	$(CC) $(ORACLE_CFLAGS) -o $@ $< $$(pkg-config --cflags --libs glib-2.0)

oracle-images: ## Build the oracle Docker images (GLib 2.68, 2.80 and newest)
	@for flavor in $(ORACLE_FLAVORS); do \
	  echo "==> $(ORACLE_IMAGE):$$flavor"; \
	  docker build -f tools/oracle/docker/$$flavor.Dockerfile -t $(ORACLE_IMAGE):$$flavor tools/oracle || exit 1; \
	done

oracle-conformance: oracle-images ## Check every oracle image against the expected outputs
	@status=0; for flavor in $(ORACLE_FLAVORS); do \
	  echo "==> $(ORACLE_IMAGE):$$flavor"; \
	  tools/oracle/run-cases.sh check $(ORACLE_DOCKER_RUN) $(ORACLE_IMAGE):$$flavor || status=1; \
	done; exit $$status

oracle-diff: oracle-images ## Differential tests of the emulators against every oracle image (key files, GOption)
	@status=0; for flavor in $(ORACLE_FLAVORS); do \
	  echo "==> $(ORACLE_IMAGE):$$flavor"; \
	  MYDUMPER_LINT_ORACLE="docker run --rm -i $(ORACLE_IMAGE):$$flavor" \
	    go test ./internal/oracletest/ -run 'TestKnownCases' -count=1 || status=1; \
	  for target in FuzzStructured FuzzBytes FuzzStructuredPlain FuzzBytesPlain; do \
	    MYDUMPER_LINT_ORACLE="docker run --rm -i $(ORACLE_IMAGE):$$flavor" \
	      go test ./internal/oracletest/ -run '^$$' -fuzz "^$$target\$$" -fuzztime $(ORACLE_FUZZTIME) || status=1; \
	  done; \
	  MYDUMPER_LINT_ORACLE="docker run --rm -i $(ORACLE_IMAGE):$$flavor" MYDUMPER_LINT_GOPTION_CASES=$(GOPTION_CASES) \
	    go test ./internal/goption/ -run 'TestOracleRandom' -count=1 || status=1; \
	done; exit $$status
