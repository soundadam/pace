#!/usr/bin/env bash
# Cross-compiles pace for every release platform and packages it into dist/,
# named per the appkit release conventions
# (https://github.com/soundadam/appkit#发布产物):
#
#   pace-<ver>-<os>-<arch>.tar.gz   macOS and Linux
#   pace-<ver>-windows-<arch>.zip   Windows
#
# each holding pace-<ver>-<os>-<arch>/pace[.exe] with README.md, LICENSE and
# THIRD_PARTY_NOTICES.md. The version is injected as main.version; the release
# workflow passes the tag without its leading "v". Linux runs the same script
# as CI, and it needs GNU tar and zip.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
    echo "usage: $0 VERSION" >&2
    exit 1
fi
version=${1#v}
extras=(README.md LICENSE THIRD_PARTY_NOTICES.md)
# Archive timestamps come from the commit, so the same commit and version
# package to the same bytes.
mtime=$(git log -1 --format=%cI)

rm -rf dist
mkdir -p dist

for platform in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do
    goos=${platform%/*}
    goarch=${platform#*/}
    case $goos in darwin) os=macos ;; *) os=$goos ;; esac
    case $goarch in amd64) arch=x86_64 ;; arm64) arch=aarch64 ;; esac
    exe=pace
    [ "$goos" = windows ] && exe=pace.exe

    name="pace-$version-$os-$arch"
    mkdir -p "dist/$name"
    CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath \
        -ldflags "-s -w -buildid= -X main.version=$version" \
        -o "dist/$name/$exe" ./cmd/pace
    cp "${extras[@]}" "dist/$name/"
    touch -d "$mtime" "dist/$name" "dist/$name"/*

    if [ "$goos" = windows ]; then
        (cd dist && TZ=UTC zip -qrX "$name.zip" "$name")
    else
        tar -C dist --sort=name --owner=0 --group=0 --numeric-owner \
            --mtime="$mtime" -cf - "$name" | gzip -n >"dist/$name.tar.gz"
    fi
    rm -rf "dist/${name:?}"
done

ls -l dist
