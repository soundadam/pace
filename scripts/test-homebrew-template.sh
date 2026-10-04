#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
workdir=$(mktemp -d "${TMPDIR:-/tmp}/pace-formula.XXXXXX")
cleanup() {
  rm -rf "$workdir"
}
trap cleanup EXIT HUP INT TERM

output="$workdir/pace.rb"
dummy_sha="0000000000000000000000000000000000000000000000000000000000000000"
umask 077
"$ROOT/scripts/render-homebrew-formula.sh" 0.1.0 "$dummy_sha" "$output" >/dev/null
python3 - "$output" <<'PY'
import pathlib
import stat
import sys

path = pathlib.Path(sys.argv[1])
mode = stat.S_IMODE(path.stat().st_mode)
if mode != 0o644:
    raise SystemExit(f"test-homebrew-template: output mode is {mode:o}, expected 644")
PY

outside_directory="$workdir/outside-formula-output"
linked_directory="$workdir/linked-formula-output"
mkdir "$outside_directory"
ln -s "$outside_directory" "$linked_directory"
if "$ROOT/scripts/render-homebrew-formula.sh" \
  0.1.0 "$dummy_sha" "$linked_directory/pace.rb" >/dev/null 2>&1; then
  echo "test-homebrew-template: renderer accepted a symlinked output directory" >&2
  exit 1
fi
if find "$outside_directory" -mindepth 1 -print | grep -q .; then
  echo "test-homebrew-template: renderer wrote through a symlinked output directory" >&2
  exit 1
fi

linked_output="$workdir/linked-output"
ln -s "$outside_directory" "$linked_output"
if "$ROOT/scripts/render-homebrew-formula.sh" \
  0.1.0 "$dummy_sha" "$linked_output" >/dev/null 2>&1; then
  echo "test-homebrew-template: renderer accepted a symlinked output path" >&2
  exit 1
fi
if find "$outside_directory" -mindepth 1 -print | grep -q .; then
  echo "test-homebrew-template: renderer wrote through a symlinked output path" >&2
  exit 1
fi

expect_invalid_version() {
  command=$1
  version=$2
  if (
    cd "$workdir"
    "$command" "$version" "$dummy_sha" "$workdir/invalid.rb"
  ) >/dev/null 2>&1; then
    echo "test-homebrew-template: accepted invalid version $version" >&2
    exit 1
  fi
}

expect_invalid_release_version() {
  version=$1
  error_output="$workdir/build-release-error.txt"
  if (
    cd "$workdir"
    "$ROOT/scripts/build-release.sh" "$version"
  ) >/dev/null 2>"$error_output"; then
    echo "test-homebrew-template: release builder accepted invalid version $version" >&2
    exit 1
  fi
  if ! grep -q 'VERSION must be numeric major.minor.patch' "$error_output"; then
    echo "test-homebrew-template: release builder did not reject version syntax $version" >&2
    cat "$error_output" >&2
    exit 1
  fi
}

# A numeric filename makes an unquoted wildcard component expand into an
# apparently valid version component. Keep this fixture so validation remains
# independent of the caller's current directory contents.
: > "$workdir/2"

for invalid_version in \
  1.2 \
  1.2.3.4 \
  1..3 \
  01.2.3 \
  1.02.3 \
  1.2.03 \
  1.2.3-rc1 \
  '1.*.3' \
  '1.?.3' \
  '1.[2].3'; do
  expect_invalid_version "$ROOT/scripts/render-homebrew-formula.sh" "$invalid_version"
done

for invalid_version in \
  1.2 \
  1.2.3.4 \
  1..3 \
  01.2.3 \
  1.2.3-rc1 \
  '1.*.3' \
  '1.?.3' \
  '1.[2].3'; do
  expect_invalid_release_version "$invalid_version"
done

if command -v ruby >/dev/null 2>&1; then
  ruby -c "$output" >/dev/null
else
  printf '%s\n' 'Ruby not installed; syntax check deferred to the macOS Homebrew gate.'
fi

grep -q 'class Pace < Formula' "$output"
grep -q 'depends_on "go" => :build' "$output"
grep -q 'cd "components/librespeed-cli"' "$output"
grep -q 'resource "ndt7-client"' "$output"
grep -q 'defs.ProgVersion=v1.0.13-campus.1' "$output"
grep -q 'assert_match '\''"ready":true' "$output"
grep -Fq 'homepage "https://github.com/soundadam/pace"' "$output"
grep -Fq 'url "https://github.com/soundadam/pace/releases/download/v0.1.0/pace-0.1.0.tar.gz"' "$output"
if grep -Eq '^[[:space:]]*head ' "$output"; then
  echo "test-homebrew-template: Formula exposes inaccessible private HEAD source" >&2
  exit 1
fi
if grep -q '@[A-Z_][A-Z_]*@' "$output"; then
  echo "test-homebrew-template: unresolved placeholder" >&2
  exit 1
fi

printf '%s\n' 'Homebrew Formula template test passed.'
