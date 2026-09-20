#!/bin/sh
# Measure statement coverage for one module and enforce a floor.
#
# Usage: run-coverage.sh <module-dir> <min-percent> [profile-name]
#
# The profile is written next to the module's go.mod as coverage.out, which
# .gitignore already covers. Prints the per-package breakdown and the module
# total, then fails if the total is below <min-percent>.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

GO=${GO:-go}
GOTOOLCHAIN=${GOTOOLCHAIN:-auto}
export GOTOOLCHAIN

if [ "$#" -lt 2 ]; then
  echo "usage: run-coverage.sh <module-dir> <min-percent> [profile-name]" >&2
  exit 2
fi

module=$1
minimum=$2
profile_name=${3:-coverage.out}

module_dir="$ROOT/$module"
if [ ! -f "$module_dir/go.mod" ]; then
  echo "run-coverage: $module is not a module (no go.mod)" >&2
  exit 1
fi

profile="$module_dir/$profile_name"

cd "$module_dir"
"$GO" test -covermode=atomic -coverprofile="$profile" ./...

total=$("$GO" tool cover -func="$profile" | awk '$1 == "total:" { sub(/%$/, "", $NF); print $NF; exit }')
if [ -z "$total" ]; then
  echo "run-coverage: could not read a total from $profile" >&2
  exit 1
fi

printf 'run-coverage: %s total statement coverage: %s%% (floor %s%%)\n' "$module" "$total" "$minimum"

# awk rather than bc: bc is not present on every minimal Linux image, awk is.
if awk -v got="$total" -v want="$minimum" 'BEGIN { exit (got + 0 < want + 0) ? 0 : 1 }'; then
  echo "run-coverage: $module coverage $total% is below the $minimum% floor." >&2
  echo "run-coverage: add tests, or lower the floor in make/common.mk deliberately." >&2
  exit 1
fi
