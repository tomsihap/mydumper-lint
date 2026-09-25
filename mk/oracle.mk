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

.PHONY: oracle oracle-images oracle-conformance

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
