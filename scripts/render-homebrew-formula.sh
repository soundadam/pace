#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TEMPLATE="$ROOT/packaging/homebrew/pace.rb.tmpl"

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
  echo "usage: $0 VERSION SOURCE_SHA256 [OUTPUT]" >&2
  exit 1
fi

version=${1#v}
source_sha256=$2
output=${3:-"$ROOT/dist/Formula/pace.rb"}

validate_version() {
  candidate=$1
  major=${candidate%%.*}
  remainder=${candidate#*.}
  if [ "$remainder" = "$candidate" ]; then
    return 1
  fi
  minor=${remainder%%.*}
  patch=${remainder#*.}
  if [ "$patch" = "$remainder" ]; then
    return 1
  fi
  case "$patch" in
    *.*) return 1 ;;
  esac

  for component in "$major" "$minor" "$patch"; do
    case "$component" in
      ""|*[!0-9]*) return 1 ;;
      0) ;;
      0*) return 1 ;;
      *) ;;
    esac
  done
}

if ! validate_version "$version"; then
  echo "render-homebrew-formula: VERSION must be numeric major.minor.patch without leading zeros" >&2
  exit 1
fi
case "$source_sha256" in
  *[!0-9a-f]*|"")
    echo "render-homebrew-formula: SOURCE_SHA256 must be lowercase hexadecimal" >&2
    exit 1
    ;;
esac
if [ "${#source_sha256}" -ne 64 ]; then
  echo "render-homebrew-formula: SOURCE_SHA256 must contain 64 characters" >&2
  exit 1
fi

output_directory=$(dirname -- "$output")
if [ -L "$output_directory" ]; then
  echo "render-homebrew-formula: output directory must not be a symbolic link: $output_directory" >&2
  exit 1
fi
if [ ! -d "$output_directory" ]; then
  if ! mkdir -p "$output_directory" 2>/dev/null || \
    [ -L "$output_directory" ] || [ ! -d "$output_directory" ]; then
    echo "render-homebrew-formula: cannot create directory $output_directory" >&2
    exit 1
  fi
fi
if [ -L "$output" ] || [ -d "$output" ]; then
  echo "render-homebrew-formula: output path must be a regular file path: $output" >&2
  exit 1
fi
temporary_output="$output.tmp.$$"
cleanup() {
  rm -f "$temporary_output"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

sed \
  -e "s/@VERSION@/$version/g" \
  -e "s/@SOURCE_SHA256@/$source_sha256/g" \
  "$TEMPLATE" > "$temporary_output"

if grep -q '@[A-Z_][A-Z_]*@' "$temporary_output"; then
  echo "render-homebrew-formula: unresolved template placeholder" >&2
  exit 1
fi
chmod 0644 "$temporary_output"
mv -f "$temporary_output" "$output"

printf 'Rendered Homebrew Formula: %s\n' "$output"
