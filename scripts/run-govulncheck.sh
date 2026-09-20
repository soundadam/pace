#!/bin/sh
# Run govulncheck over one or more modules.
#
# Usage: run-govulncheck.sh [module-dir ...]   (default: the root module)
#
# Static analysis only. The single piece of network traffic is the fetch of the
# Go vulnerability database, so results can change without any code changing.
# Exits non-zero on the first module with an affecting vulnerability.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

GO=${GO:-go}
GOTOOLCHAIN=${GOTOOLCHAIN:-auto}
PINNED_VERSION=${GOVULNCHECK_VERSION:-}
export GOTOOLCHAIN

# Prefer whatever is on PATH; fall back to the GOBIN/GOPATH location, which is
# where `make tools-vuln` puts it and which is not on PATH on every machine.
resolve_govulncheck() {
  if command -v govulncheck >/dev/null 2>&1; then
    command -v govulncheck
    return 0
  fi

  gobin=$("$GO" env GOBIN 2>/dev/null || true)
  if [ -z "$gobin" ]; then
    gopath=$("$GO" env GOPATH 2>/dev/null || true)
    [ -n "$gopath" ] && gobin="$gopath/bin"
  fi
  if [ -n "$gobin" ] && [ -x "$gobin/govulncheck" ]; then
    printf '%s\n' "$gobin/govulncheck"
    return 0
  fi

  return 1
}

if ! govulncheck_bin=$(resolve_govulncheck); then
  echo "run-govulncheck: govulncheck is not installed." >&2
  echo "run-govulncheck: install the pinned version with" >&2
  echo >&2
  echo "    make tools-vuln" >&2
  echo >&2
  if [ -n "$PINNED_VERSION" ]; then
    echo "run-govulncheck: (equivalent to: go install golang.org/x/vuln/cmd/govulncheck@$PINNED_VERSION)" >&2
  else
    echo "run-govulncheck: (equivalent to: go install golang.org/x/vuln/cmd/govulncheck@latest)" >&2
  fi
  echo "run-govulncheck: if it is already installed, add \"\$($GO env GOPATH)/bin\" to PATH." >&2
  exit 1
fi

# The scanner version is pinned in make/common.mk and CI installs it from there,
# so a mismatch here means the local install has drifted. Warn rather than fail:
# an out-of-date scanner still finds most things, and refusing to run would be a
# worse outcome than a noisy run.
if [ -n "$PINNED_VERSION" ]; then
  installed=$("$govulncheck_bin" -version 2>/dev/null | awk '/^Scanner:/ { print $2; exit }' || true)
  installed=${installed#govulncheck@}
  if [ -n "$installed" ] && [ "$installed" != "$PINNED_VERSION" ]; then
    echo "run-govulncheck: warning: using govulncheck $installed but make/common.mk pins $PINNED_VERSION; run 'make tools-vuln' to match CI." >&2
  fi
fi

if [ "$#" -eq 0 ]; then
  set -- .
fi

for module in "$@"; do
  module_dir="$ROOT/$module"
  if [ ! -f "$module_dir/go.mod" ]; then
    echo "run-govulncheck: $module is not a module (no go.mod)" >&2
    exit 1
  fi
  printf 'run-govulncheck: scanning %s\n' "$module"
  (cd "$module_dir" && "$govulncheck_bin" ./...)
done
