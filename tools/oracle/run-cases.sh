#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Runs the oracle cases through an oracle command. Prints the results in the
# format of the expected files, or checks them against those files.
#
# usage: tools/oracle/run-cases.sh MODE ORACLE_CMD [ARG...]
#
#   text     testdata/oracle-cases/*.cnf, text mode   (format of expected.txt)
#   json     testdata/oracle-cases/*.cnf, --json      (format of expected.jsonl)
#   goption  testdata/goption-cases/*.cases           (format of expected.jsonl)
#   check    the three above, plus --goption against testdata/goption-expected.txt;
#            prints the GLib version and exits 1 on any difference
#
# ORACLE_CMD runs the oracle, for example:
#   bin/oracle
#   docker run --rm -i -v "$PWD:/w:ro" -w /w mydumper-lint-oracle:alma9
# Run from the repository root: case files are passed as paths relative to it,
# so a container must mount the repository as its working directory.
#
# Environment of each case: the locale variables below are unset, then every
# NAME=value line of the sidecar file NN_name.env (if present) is set. Both
# travel as oracle arguments (--unsetenv, --setenv), so they also reach a
# containerized oracle.
#
# json mode drops the "glib" field and adds "case" as the first key: the
# expected JSON does not depend on the GLib version.

set -eu
LC_ALL=C # deterministic glob order; the oracle itself gets --unsetenv LC_ALL
export LC_ALL

CASES=testdata/oracle-cases
GOPTION_CASES=testdata/goption-cases
GOPTION_PROBE=testdata/goption-expected.txt
# Variables that change what GLib or the C library read from the environment.
LOCALE_VARS='LANG LANGUAGE LC_ALL LC_MESSAGES LC_CTYPE CHARSET'
NL='
'
CR=$(printf '\r')

usage() {
  echo 'usage: tools/oracle/run-cases.sh text|json|goption|check ORACLE_CMD [ARG...]' >&2
  exit 2
}

die() {
  printf 'run-cases.sh: %s\n' "$*" >&2
  exit 2
}

# run_oracle ENVFILE N ORACLE_CMD... ARG1..ARGN
# Runs ORACLE_CMD, then the environment options, then the N arguments, with
# stdin from /dev/null (so that "docker run -i" never eats the script's input).
run_oracle() {
  ro_env=$1
  ro_n=$2
  shift 2
  # ORACLE_CMD ARGS -> ARGS ORACLE_CMD
  ro_m=$(($# - ro_n))
  while [ "$ro_m" -gt 0 ]; do
    set -- "$@" "$1"
    shift
    ro_m=$((ro_m - 1))
  done
  # ARGS ORACLE_CMD -> ARGS ORACLE_CMD ENV
  for ro_var in $LOCALE_VARS; do
    set -- "$@" --unsetenv "$ro_var"
  done
  if [ -n "$ro_env" ] && [ -f "$ro_env" ]; then
    while IFS= read -r ro_line || [ -n "$ro_line" ]; do
      case $ro_line in
      '' | '#'*) continue ;;
      *"$CR"*) die "$ro_env: carriage return in: $ro_line" ;;
      esac
      case ${ro_line%%=*} in
      "$ro_line" | '' | [0-9]* | *[!A-Za-z0-9_]*) die "$ro_env: expected NAME=value, got: $ro_line" ;;
      esac
      set -- "$@" --setenv "$ro_line"
    done <"$ro_env"
  fi
  # ARGS ORACLE_CMD ENV -> ORACLE_CMD ENV ARGS
  while [ "$ro_n" -gt 0 ]; do
    set -- "$@" "$1"
    shift
    ro_n=$((ro_n - 1))
  done
  "$@" </dev/null
}

# Case names end up in JSON and in sed-free string surgery: keep them plain.
check_name() {
  case $1 in
  *[!A-Za-z0-9._-]*) die "unsupported character in case file name: $1" ;;
  esac
}

text_cases() {
  for f in "$CASES"/*.cnf; do
    [ -f "$f" ] || die "no case in $CASES"
    name=${f##*/}
    check_name "$name"
    status=0
    run_oracle "${f%.cnf}.env" 1 "$@" "$f" >"$tmp/out" || status=$?
    # 0: loadable, 1: rejected by GLib; anything else is an oracle failure.
    [ "$status" -le 1 ] || die "$name: the oracle exited with status $status"
    printf '### %s\n' "$name"
    cat "$tmp/out"
  done
}

json_cases() {
  for f in "$CASES"/*.cnf; do
    [ -f "$f" ] || die "no case in $CASES"
    name=${f##*/}
    check_name "$name"
    status=0
    run_oracle "${f%.cnf}.env" 2 "$@" --json "$f" >"$tmp/out" || status=$?
    [ "$status" -eq 0 ] || die "$name: the oracle exited with status $status"
    line=$(cat "$tmp/out")
    case $line in
    *"$NL"*) die "$name: --json printed more than one line" ;;
    '{"glib":"'*'",'*) ;;
    *) die "$name: unexpected --json output: $line" ;;
    esac
    printf '%s\n' "$line" | cmp -s - "$tmp/out" || die "$name: --json output does not end with a newline"
    rest=${line#'{"glib":"'*'",'}
    printf '{"case":"%s",%s\n' "$name" "$rest"
  done
}

goption_cases() {
  n=1
  for f in "$GOPTION_CASES"/*.cases; do
    [ -f "$f" ] || die "no case file in $GOPTION_CASES"
    n=$((n + 1))
  done
  status=0
  run_oracle '' "$n" "$@" --goption-cases "$GOPTION_CASES"/*.cases || status=$?
  [ "$status" -eq 0 ] || die "--goption-cases: the oracle exited with status $status"
}

# compare EXPECTED ACTUAL LABEL
compare() {
  if cmp -s "$1" "$2"; then
    printf '  %-8s ok\n' "$3"
  else
    printf '  %-8s DIFFERS from %s\n' "$3" "$1"
    diff -a -u "$1" "$2" | sed 's/^/    /' || true
    failed=1
  fi
}

check() {
  version=$(run_oracle '' 1 "$@" --glib-version) || die "--glib-version failed"
  echo "  GLib     $version"
  failed=0
  text_cases "$@" >"$tmp/expected.txt"
  compare "$CASES/expected.txt" "$tmp/expected.txt" text
  json_cases "$@" >"$tmp/expected.jsonl"
  compare "$CASES/expected.jsonl" "$tmp/expected.jsonl" json
  goption_cases "$@" >"$tmp/goption.jsonl"
  compare "$GOPTION_CASES/expected.jsonl" "$tmp/goption.jsonl" goption
  status=0
  run_oracle '' 1 "$@" --goption >"$tmp/goption-probe.txt" || status=$?
  [ "$status" -eq 0 ] || die "--goption: the oracle exited with status $status"
  compare "$GOPTION_PROBE" "$tmp/goption-probe.txt" probe
  return "$failed"
}

[ $# -ge 2 ] || usage
mode=$1
shift
[ -d "$CASES" ] || die "run from the repository root"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/run-cases.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

case $mode in
text) text_cases "$@" ;;
json) json_cases "$@" ;;
goption) goption_cases "$@" ;;
check) check "$@" ;;
*) usage ;;
esac
