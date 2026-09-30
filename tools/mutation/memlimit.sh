#!/usr/bin/env bash
# Runs a program with its data segment capped: `make mutation` passes it to
# go test as -exec (mk/slow.mk), so it wraps every test binary gremlins runs.
#
# Some mutants turn a loop into one that appends forever (i++ into i-- in the
# pre-processor's copy loop). gremlins stops a mutant's `go test` at its
# timeout, but not the test binary it started, which keeps allocating: on a
# GitHub runner the machine runs out of memory and the runner itself is shut
# down, failing the whole job. Under the cap the Go runtime stops with "out of
# memory" within seconds, the test fails, and the mutant counts as killed.
#
# Linux enforces RLIMIT_DATA on memory mappings (since 4.7). macOS does not:
# there the cap is a no-op, and swap absorbs the growth until the timeout.
ulimit -d "${MUTATION_MEMLIMIT_KB:-2097152}" 2>/dev/null || true
exec "$@"
