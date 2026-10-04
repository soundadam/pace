#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
GO_VERSION="1.25.8"
GO_ARCHIVE="go${GO_VERSION}.linux-amd64.tar.gz"
GO_ARCHIVE_SHA256="ceb5e041bbc3893846bd1614d76cb4681c91dadee579426cf21a63f2d7e03be6"
GO_ARCHIVE_URL="https://go.dev/dl/${GO_ARCHIVE}"

checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "run-local-ci: sha256sum or shasum is required" >&2
    exit 1
  fi
}

fetch() {
  url=$1
  output=$2
  if ! curl -fsSL --retry 3 --retry-all-errors --connect-timeout 15 --max-time 300 -o "$output" "$url"; then
    curl -4 -fsSL --retry 3 --retry-all-errors --connect-timeout 15 --max-time 300 -o "$output" "$url"
  fi
}

apt_get_with_lock_retry() {
  attempts=0
  log_file=$(mktemp "${TMPDIR:-/tmp}/pace-apt.XXXXXX")

  while :; do
    attempts=$((attempts + 1))
    # The log is intentionally opened by the invoking user; mktemp created it in a user-writable directory.
    # shellcheck disable=SC2024
    if sudo env DEBIAN_FRONTEND=noninteractive \
      apt-get -o DPkg::Lock::Timeout=120 "$@" >"$log_file" 2>&1; then
      rm -f "$log_file"
      return 0
    fi

    if [ "$attempts" -ge 60 ] || ! grep -Eq \
      'Could not get lock|Unable to acquire the dpkg frontend lock|Unable to lock directory' \
      "$log_file"; then
      cat "$log_file" >&2
      rm -f "$log_file"
      return 1
    fi

    if [ "$attempts" -eq 1 ]; then
      echo "run-local-ci: waiting for the guest package manager lock" >&2
    fi
    sleep 2
  done
}

install_exact_go_archive() {
  tools_root="$ROOT/.tools/go-${GO_VERSION}-linux-amd64"
  go_bin="$tools_root/go/bin/go"
  if [ ! -x "$go_bin" ]; then
    command -v curl >/dev/null 2>&1 || {
      echo "run-local-ci: curl is required to bootstrap Go" >&2
      exit 1
    }
    command -v tar >/dev/null 2>&1 || {
      echo "run-local-ci: tar is required to bootstrap Go" >&2
      exit 1
    }

    workdir=$(mktemp -d "${TMPDIR:-/tmp}/pace-go.XXXXXX")
    cleanup() {
      rm -rf "$workdir"
    }
    trap cleanup EXIT HUP INT TERM

    archive="$workdir/$GO_ARCHIVE"
    extract_root="$workdir/extract"
    fetch "$GO_ARCHIVE_URL" "$archive"
    actual=$(checksum "$archive")
    if [ "$actual" != "$GO_ARCHIVE_SHA256" ]; then
      echo "run-local-ci: Go archive checksum mismatch: got $actual, want $GO_ARCHIVE_SHA256" >&2
      exit 1
    fi
    if tar -tzf "$archive" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
      echo "run-local-ci: Go archive contains an unsafe path" >&2
      exit 1
    fi

    mkdir -p "$extract_root" "$ROOT/.tools"
    tar -xzf "$archive" -C "$extract_root"
    test -x "$extract_root/go/bin/go"
    rm -rf "$tools_root"
    mv "$extract_root" "$tools_root"
    trap - EXIT HUP INT TERM
    cleanup
  fi
  printf '%s\n' "$go_bin"
}

ensure_go() {
  if command -v go >/dev/null 2>&1; then
    detected=$(GOTOOLCHAIN=local go env GOVERSION 2>/dev/null || true)
    if [ "$detected" = "go${GO_VERSION}" ]; then
      command -v go
      return
    fi
  fi

  os=$(uname -s)
  arch=$(uname -m)
  if [ "$os" != "Linux" ] || { [ "$arch" != "x86_64" ] && [ "$arch" != "amd64" ]; }; then
    echo "run-local-ci: Go ${GO_VERSION} is required; automatic bootstrap supports only Linux amd64" >&2
    exit 1
  fi

  install_exact_go_archive
}

ensure_c_compiler() {
  if command -v cc >/dev/null 2>&1; then
    return
  fi
  if command -v sudo >/dev/null 2>&1 && command -v apt-get >/dev/null 2>&1; then
    apt_get_with_lock_retry update
    apt_get_with_lock_retry install --yes --no-install-recommends build-essential
  fi
  if ! command -v cc >/dev/null 2>&1; then
    echo "run-local-ci: a C compiler is required for the Go race detector" >&2
    exit 1
  fi
}

GO_BIN=$(ensure_go)
SELECTED_GO_VERSION=$(GOTOOLCHAIN=local "$GO_BIN" env GOVERSION)
if [ "$SELECTED_GO_VERSION" != "go${GO_VERSION}" ]; then
  echo "run-local-ci: selected $SELECTED_GO_VERSION, expected go${GO_VERSION}" >&2
  exit 1
fi
printf 'run-local-ci: using %s\n' "$SELECTED_GO_VERSION"
PATH=$(dirname "$GO_BIN"):$PATH
ensure_c_compiler
GOCACHE="$ROOT/.tools/go-build-cache"
GOENV=off
GOTOOLCHAIN=local
CGO_ENABLED=1
export PATH GOCACHE GOENV GOTOOLCHAIN CGO_ENABLED
mkdir -p "$GOCACHE"

exec make -C "$ROOT" GO=go GOTOOLCHAIN=local verify-mod test-offline test-race build
