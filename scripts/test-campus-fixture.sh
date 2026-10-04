#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
BINARY="$ROOT/bin/pace"
IPV4_FIXTURE="$ROOT/internal/provider/campus/testdata/librespeed-success-ipv4.json"
IPV6_FIXTURE="$ROOT/internal/provider/campus/testdata/librespeed-success-ipv6.json"

mkdir -p "$ROOT/bin"
(
  cd "$ROOT"
  go build -o "$BINARY" ./cmd/pace
)

workdir=$(mktemp -d "${TMPDIR:-/tmp}/pace-fixture.XXXXXX")
cleanup() {
  rm -rf "$workdir"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$workdir/app/bin" "$workdir/helpers" "$workdir/home" "$workdir/work"
cp "$BINARY" "$workdir/app/bin/pace"

cat > "$workdir/helpers/librespeed-cli" <<EOF
#!/bin/sh
if [ "\${1:-}" = "--version" ]; then
  printf 'librespeed-cli v1.0.13-campus.1 (built on fixture)\n'
  exit 0
fi
if [ -n "\${PACE_FIXTURE_SLEEP:-}" ]; then
  exec sleep "\$PACE_FIXTURE_SLEEP"
fi
printf '%s\n' '{"type":"progress","test":"download","elapsed_ms":1000,"bytes":6250000,"mbps":50}' >&2
printf '%s\n' '{"type":"progress","test":"upload","elapsed_ms":1000,"bytes":625000,"mbps":5}' >&2
for argument in "\$@"; do
  if [ "\$argument" = "--ipv6" ]; then
    cat "$IPV6_FIXTURE"
    exit 0
  fi
done
cat "$IPV4_FIXTURE"
EOF
chmod 0755 "$workdir/helpers/librespeed-cli"

run_fixture() {
  family=$1
  output=$2
  shift 2
  (
    cd "$workdir/work"
    HOME="$workdir/home" PATH="$workdir/helpers:$PATH" \
      "$workdir/app/bin/pace" campus --no-save --json "$@"
  ) > "$output"
  if LC_ALL=C grep -q "$(printf '\033')" "$output"; then
    echo "fixture test: ANSI escape found in JSON output" >&2
    exit 1
  fi
  grep -q '"status":"success"' "$output"
  grep -q "\"provider\":\"nju-campus-$family\"" "$output"
  grep -q "\"targets\":\[\"nju-campus-$family\"\]" "$output"
  grep -q "\"ipFamily\":\"$family\"" "$output"
  grep -q '"helperVersion":"v1.0.13-campus.1"' "$output"
}

run_fixture ipv4 "$workdir/ipv4.json"
run_fixture ipv6 "$workdir/ipv6.json" --ipv6

(
  cd "$workdir/work"
  exec env HOME="$workdir/home" PATH="$workdir/helpers:$PATH" PACE_FIXTURE_SLEEP=30 \
    "$workdir/app/bin/pace" campus --no-save --json
) > "$workdir/cancelled.json" &
cancel_pid=$!
sleep 0.1
kill -INT "$cancel_pid"
set +e
wait "$cancel_pid"
cancel_exit=$?
set -e
if [ "$cancel_exit" -ne 130 ]; then
  echo "fixture test: cancelled exit code was $cancel_exit, expected 130" >&2
  exit 1
fi
grep -q '"status":"cancelled"' "$workdir/cancelled.json"
grep -q '"stage":"cancelled"' "$workdir/cancelled.json"

printf '%s\n' 'Offline campus CLI fixture test passed.'
printf '%s\n' 'IPv4 result:'
cat "$workdir/ipv4.json"
printf '%s\n' 'IPv6 result:'
cat "$workdir/ipv6.json"
