#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TOOLS_DIR="$ROOT/.tools/bin"

LIBRESPEED_HELPER_VERSION="v1.0.13-campus.1"
LIBRESPEED_SOURCE="$ROOT/components/librespeed-cli"

NDT7_VERSION="v0.10.1"
NDT7_ARCHIVE_SHA256="31b40268bd7a9d31bdb5507b7ade2fad2efb8abb9e7339d2f59e9cdee5340bef"
NDT7_ARCHIVE_URL="https://codeload.github.com/m-lab/ndt7-client-go/tar.gz/refs/tags/$NDT7_VERSION"

command -v curl >/dev/null 2>&1 || {
  echo "install-dev-tools: curl is required" >&2
  exit 1
}
command -v go >/dev/null 2>&1 || {
  echo "install-dev-tools: Go is required" >&2
  exit 1
}
command -v tar >/dev/null 2>&1 || {
  echo "install-dev-tools: tar is required" >&2
  exit 1
}

checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "install-dev-tools: sha256sum or shasum is required" >&2
    exit 1
  fi
}

fetch() {
  url=$1
  output=$2
  if ! curl -fsSL --retry 3 --connect-timeout 10 --max-time 120 -o "$output" "$url"; then
    curl -4 -fsSL --retry 3 --connect-timeout 10 --max-time 120 -o "$output" "$url"
  fi
}

verify_archive() {
  archive=$1
  expected=$2
  actual=$(checksum "$archive")
  if [ "$actual" != "$expected" ]; then
    echo "install-dev-tools: archive checksum mismatch: got $actual, want $expected" >&2
    exit 1
  fi
}

mkdir -p "$TOOLS_DIR"
workdir=$(mktemp -d "${TMPDIR:-/tmp}/pace-tools.XXXXXX")
cleanup() {
  rm -rf "$workdir"
}
trap cleanup EXIT HUP INT TERM

librespeed_binary="$workdir/librespeed-cli"
librespeed_ldflags="-s -w -buildid= -X github.com/librespeed/speedtest-cli/defs.ProgName=librespeed-cli -X github.com/librespeed/speedtest-cli/defs.ProgVersion=$LIBRESPEED_HELPER_VERSION -X github.com/librespeed/speedtest-cli/defs.BuildDate=1970-01-01T00:00:00Z"
(
  cd "$LIBRESPEED_SOURCE"
  GOTOOLCHAIN=auto go build -trimpath -ldflags "$librespeed_ldflags" -o "$librespeed_binary" ./
)
install -m 0755 "$librespeed_binary" "$TOOLS_DIR/.librespeed-cli.$$"
mv -f "$TOOLS_DIR/.librespeed-cli.$$" "$TOOLS_DIR/librespeed-cli"

first_line=$("$TOOLS_DIR/librespeed-cli" --version | sed -n '1p')
case "$first_line" in
  "librespeed-cli $LIBRESPEED_HELPER_VERSION "*) ;;
  *)
    echo "install-dev-tools: unexpected LibreSpeed version output: $first_line" >&2
    exit 1
    ;;
esac

ndt7_archive="$workdir/ndt7-client.tar.gz"
ndt7_source="$workdir/ndt7-client"
fetch "$NDT7_ARCHIVE_URL" "$ndt7_archive"
verify_archive "$ndt7_archive" "$NDT7_ARCHIVE_SHA256"
mkdir -p "$ndt7_source"
tar -xzf "$ndt7_archive" --strip-components=1 -C "$ndt7_source"

ndt7_binary="$workdir/ndt7-client-bin"
ndt7_ldflags="-s -w -buildid= -X main.ClientName=pace -X main.ClientVersion=0.10.1"
(
  cd "$ndt7_source"
  GOTOOLCHAIN=auto go build -trimpath -ldflags "$ndt7_ldflags" -o "$ndt7_binary" ./cmd/ndt7-client
)
install -m 0755 "$ndt7_binary" "$TOOLS_DIR/.ndt7-client.$$"
mv -f "$TOOLS_DIR/.ndt7-client.$$" "$TOOLS_DIR/ndt7-client"
printf '%s\n' "$NDT7_VERSION" > "$TOOLS_DIR/.ndt7-client.version.$$"
chmod 0600 "$TOOLS_DIR/.ndt7-client.version.$$"
mv -f "$TOOLS_DIR/.ndt7-client.version.$$" "$TOOLS_DIR/ndt7-client.version"

printf 'Installed %s at %s\n' "$first_line" "$TOOLS_DIR/librespeed-cli"
printf 'Installed ndt7-client %s at %s\n' "$NDT7_VERSION" "$TOOLS_DIR/ndt7-client"
