#!/bin/sh
# Go version pinning is consistent.
#
# go.mod is the single source of truth. Anything else that names a Go version
# must agree with it, so the three pins cannot drift apart.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

root_version=$(awk '$1 == "go" { print $2; exit }' "$ROOT/go.mod")
component_version=$(awk '$1 == "go" { print $2; exit }' "$ROOT/components/librespeed-cli/go.mod")
local_ci_version=$(awk -F'"' '/^GO_ARCHIVE_PINNED_VERSION=/ { print $2; exit }' "$ROOT/scripts/run-local-ci.sh")

printf 'go.mod=%s component=%s run-local-ci.sh=%s\n' \
  "$root_version" "$component_version" "$local_ci_version"

if [ -z "$root_version" ]; then
  echo "check-go-version-pins: could not read the go directive from go.mod" >&2
  exit 1
fi

status=0
if [ "$component_version" != "$root_version" ]; then
  echo "check-go-version-pins: components/librespeed-cli/go.mod pins Go $component_version but go.mod pins $root_version." >&2
  status=1
fi
if [ "$local_ci_version" != "$root_version" ]; then
  echo "check-go-version-pins: scripts/run-local-ci.sh pins Go $local_ci_version but go.mod pins $root_version." >&2
  echo "check-go-version-pins: update GO_ARCHIVE_PINNED_VERSION and GO_ARCHIVE_SHA256 from https://go.dev/dl/ to match go.mod." >&2
  status=1
fi

if [ "$status" -ne 0 ]; then
  echo "check-go-version-pins: Go version pins disagree; go.mod is authoritative." >&2
fi
exit "$status"
