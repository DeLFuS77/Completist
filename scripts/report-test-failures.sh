#!/usr/bin/env bash
# Prints the failed tests, their messages, panics and data races of a "go test" log as GitHub
# annotations, so a failure can be read from the run summary.
log="${1:-test.log}"
[ -f "$log" ] || exit 0
grep -E -A8 -- '--- FAIL|^panic:|WARNING: DATA RACE|^FAIL[[:space:]]' "$log" \
	| grep -vE '^--$' | head -60 \
	| while IFS= read -r line; do
		line="${line//'%'/'%25'}"
		echo "::error::${line}"
	done
exit 0
