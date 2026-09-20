#!/bin/sh
# Run golangci-lint over one or more modules, report only.
#
# Usage: run-golangci-lint.sh [module-dir ...]   (default: the root module)
#
# This script never fails the build. golangci-lint is being introduced in
# report-only mode: the findings are for a human to read and triage, not a gate.
# Set GOLANGCI_LINT_EXTRA_LINTERS to a comma-separated list to layer additional
# linters on top of .golangci.yml without editing the committed configuration.
set -u

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

GO=${GO:-go}
GOTOOLCHAIN=${GOTOOLCHAIN:-auto}
PINNED_VERSION=${GOLANGCI_LINT_VERSION:-}
EXTRA_LINTERS=${GOLANGCI_LINT_EXTRA_LINTERS:-}
export GOTOOLCHAIN

resolve_golangci_lint() {
  if command -v golangci-lint >/dev/null 2>&1; then
    command -v golangci-lint
    return 0
  fi

  gobin=$("$GO" env GOBIN 2>/dev/null || true)
  if [ -z "$gobin" ]; then
    gopath=$("$GO" env GOPATH 2>/dev/null || true)
    [ -n "$gopath" ] && gobin="$gopath/bin"
  fi
  if [ -n "$gobin" ] && [ -x "$gobin/golangci-lint" ]; then
    printf '%s\n' "$gobin/golangci-lint"
    return 0
  fi

  return 1
}

if ! lint_bin=$(resolve_golangci_lint); then
  echo "run-golangci-lint: golangci-lint is not installed." >&2
  echo "run-golangci-lint: install the pinned version with" >&2
  echo >&2
  echo "    make tools-lint" >&2
  echo >&2
  if [ -n "$PINNED_VERSION" ]; then
    echo "run-golangci-lint: (equivalent to: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$PINNED_VERSION)" >&2
  fi
  echo "run-golangci-lint: reporting nothing; this check never fails the build." >&2
  exit 0
fi

if [ "$#" -eq 0 ]; then
  set -- .
fi

for module in "$@"; do
  module_dir="$ROOT/$module"
  if [ ! -f "$module_dir/go.mod" ]; then
    echo "run-golangci-lint: $module is not a module (no go.mod); skipping" >&2
    continue
  fi
  printf '\nrun-golangci-lint: %s' "$module"
  if [ -n "$EXTRA_LINTERS" ]; then
    printf ' (plus %s)' "$EXTRA_LINTERS"
  fi
  printf '\n'

  if [ -n "$EXTRA_LINTERS" ]; then
    (cd "$module_dir" && "$lint_bin" run --enable="$EXTRA_LINTERS" ./...) || true
  else
    (cd "$module_dir" && "$lint_bin" run ./...) || true
  fi
done

echo
echo "run-golangci-lint: report only; findings above do not fail the build."
exit 0
