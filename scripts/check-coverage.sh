#!/usr/bin/env bash
set -euo pipefail

profile="${1:-coverage.out}"
if [[ ! -f "$profile" ]]; then
    echo "coverage profile not found: $profile" >&2
    exit 2
fi

# Strip test-support packages from the profile before counting.
filtered="$(mktemp)"
trap 'rm -f "$filtered"' EXIT

# Keep the "mode:" header, drop lines belonging to test-support packages.
awk '
    NR == 1 { print; next }
    $0 !~ /\/internal\/rabbitmq\/rabbitmqtest\// && $0 !~ /\/internal\/integration\// { print }
' "$profile" > "$filtered"

# Print per-function coverage, highlighting < 100%.
go tool cover -func="$filtered" | tee /dev/stderr | awk '
    /^total:/ {
        total = $NF
        exit
    }
    $NF != "100.0%" && $NF != "100%" {
        under++
    }
    END {
        if (under > 0) {
            printf "\n%d function(s) below 100%%\n", under > "/dev/stderr"
        }
        if (total != "100.0%") {
            printf "\ntotal coverage %s (want 100.0%%)\n", total > "/dev/stderr"
            exit 1
        }
        exit 0
    }
'
