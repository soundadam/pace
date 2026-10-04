# Testing pace

Routine tests never contact real NJU, M-Lab, or domestic bandwidth servers.
Real measurements are explicit operator acceptance steps.

## 1. Complete offline gate

```sh
make test-offline
GOTOOLCHAIN=auto go test -race ./...
```

The offline gate runs package tests, vet, executable-level mock-helper fixtures,
Homebrew template checks, and deterministic release artifact tests. It covers:

- station registry and IPv4/IPv6/dual expansion;
- explicit provider IDs such as `nju-campus-ipv4` and `tongji-ipv4`;
- selector recommendation, target toggling, family changes, and cancellation;
- sequential multi-target execution and skipped targets after cancellation;
- LibreSpeed arguments with telemetry disabled;
- M-Lab JSON events, transient live rates, and final summaries;
- Apple `networkQuality` JSON success/error/timeout fixtures, `-I` binding, and
  responsiveness RPM parsing;
- official Ookla JSON/server metadata, version identity, interface binding, and
  rejection of the Python `speedtest-cli`;
- success, partial, failure, timeout, malformed output, and cancellation;
- legacy schema-v1 history without an explicit `targets` field;
- normalized one-measurement-per-row CSV export;
- Bubble Tea inline rendering and cursor restoration;
- JSON and redirected output without ANSI or provider event leakage;
- atomic storage and `0700`/`0600` modes;
- deterministic source archive and Formula generation.

Focused tests:

```sh
GOTOOLCHAIN=auto go test ./internal/target ./internal/ui ./internal/provider/...
```

On Linux amd64, the reproducible KVM gate remains:

```sh
make ci
```

CI must not run `pace stations` because that command intentionally performs
real lightweight reachability probes.

## 2. Build and helpers

```sh
make tools
make build
./bin/pace doctor --json
```

Expected helper versions:

```sh
.tools/bin/librespeed-cli --version | head -1
cat .tools/bin/ndt7-client.version
```

```text
librespeed-cli v1.0.13-campus.1 (...)
v0.10.1
```

Helper discovery order:

1. installed `../libexec/pace`;
2. repository-local `.tools/bin`;
3. explicit developer PATH fallback.

## 3. M-Lab consent

```sh
./bin/pace consent status
./bin/pace consent accept
./bin/pace consent status
```

Acceptance requires a terminal and the exact word `accept`. A noninteractive
plan containing M-Lab must fail before contacting it when consent is absent. A
plan without M-Lab must not ask for M-Lab consent.

## 4.1 Apple and Ookla acceptance

Offline tests use fake executables and never perform a real bandwidth test. On an
acceptance host, verify helper availability first. Apple is expected only on
macOS; Linux and Windows should report it as an unavailable optional provider
without failing the base run:

```sh
./bin/pace doctor --json
/usr/bin/networkQuality -h  # macOS only
speedtest --version          # only when the official Ookla CLI was installed intentionally
```

Apple must run once with `-c -s` (and `-I <active-interface>` when known), and its
JSON must expose throughput, base RTT and RPM fields. Ookla must run once with
`--format=json`; inspect server ID, sponsor, host/address, latency, jitter and
address family. pace never passes `--accept-license` or `--accept-gdpr`.
A Python `speedtest-cli` binary must be reported as unavailable rather than
executed. A combined `run` must remove that optional target and continue with
the remaining providers; an explicit `pace ookla` command must fail before
starting a measurement.

For an interactive explicit `pace ookla` failure, use a fake Homebrew
resolver and command runner to verify that the official tap/update/install
sequence is displayed and runs only after an empty Enter line. Verify that a
non-empty response performs no external command, that `--json` never prompts,
and that uninstall commands are displayed only as manual conflict recovery.

## 5. Station discovery and selector

Station probes are lightweight but real:

```sh
./bin/pace stations
./bin/pace stations --json
```

Verify that every registry entry has a family/status row and M-Lab is shown as
automatic rather than as a pinned server.

Run the selector:

```sh
./bin/pace
```

Check:

- a fresh preferences path opens Chinese/English setup and saves mode `0600`;
- `pace setup` can change language and daily stations;
- later bare runs show only the configured daily stations;
- the selector clears before progress begins;
- NJU Campus plus M-Lab and Apple are recommended; Ookla is never auto-selected;
- `nju-edge` and `qlu` do not appear in the selector or setup catalogs;
- `4`, `6`, and `d` switch family modes;
- IPv4-only domestic stations are disabled in IPv6 mode;
- Space toggles stations and Enter starts the exact visible order;
- q, Esc, and Ctrl-C cancel without creating a history entry.

## 6. Real NJU acceptance

These commands perform real uploads and downloads.

### Campus

```sh
./bin/pace campus --ipv4 --no-save --json
./bin/pace campus --ipv6 --no-save --json
```

Expected target IDs:

```text
nju-campus-ipv4
nju-campus-ipv6
```

The IPv6 result must never contain an IPv4 family or server.

Unknown retired IDs must fail before measurement:

```sh
./bin/pace run --targets nju-edge --family dual --no-save --json
./bin/pace run --targets qlu --family ipv4 --no-save --json
./bin/pace edge --no-save --json
```

These must exit `1` as unknown targets/commands. They must not appear in
`pace stations`.

## 7. Domestic station acceptance

CERNET remains an explicit compatibility probe and is expected to fail cleanly
while its current backend is unreachable:

```sh
./bin/pace run --targets cernet --family ipv4 --no-save --json
./bin/pace run --targets tongji --family ipv4 --no-save --json
```

Then validate sequential batch behavior:

```sh
./bin/pace domestic --no-save --json
```

Expected target order:

```json
["tongji-ipv4"]
```

One failed station must not stop later stations. Inspect helper arguments during
offline tests to ensure `--telemetry-level disabled` is always present.

## 8. M-Lab and mixed plans

```sh
./bin/pace mlab --no-save --json
./bin/pace apple --no-save --json
./bin/pace run --targets nju-campus,mlab,apple --family ipv4 --no-save --json
./bin/pace run --targets nju-campus,mlab,apple --family dual --no-save --json
```

M-Lab uses automatic Locate selection. During a TTY run its download and upload
rates should update within the same fixed-height panel. NJU or domestic failures
must not prevent M-Lab from running when it is later in the selected plan.

## 9. Terminal rendering

For an interactive combined plan, verify:

- explicit labels such as `NJU Campus · IPv6`;
- one equal four-row panel per target;
- fixed panel height while live M-Lab events arrive;
- observed LibreSpeed rates with no fabricated samples or percentage;
- no alternate-screen enter/leave sequences;
- one durable final summary after the live block clears;
- cursor hidden during rendering and restored at completion.

For redirected output:

```sh
./bin/pace run --targets nju-campus --family dual --no-save > /tmp/plain.txt
```

`/tmp/plain.txt` must have no ANSI bytes and no raw JSON.

For JSON:

```sh
./bin/pace run --targets nju-campus --family dual --no-save --json > /tmp/run.json
python3 -m json.tool /tmp/run.json
```

The file must contain exactly one JSON document.

## 10. Cancellation

Start a multi-target plan and press Ctrl-C during an active target:

```sh
./bin/pace run --targets nju-campus,mlab --family dual
```

Expected exit code: `130`. The active target is cancelled, every later target is
skipped, and no later helper starts.

## 11. History and export

Save a labeled multi-target run:

```sh
./bin/pace run \
  --targets nju-campus,mlab \
  --family ipv4 \
  --label daily \
  --note "home Wi-Fi"
```

Read it back:

```sh
./bin/pace last --json
./bin/pace history --limit 10
./bin/pace show RUN_ID --json
```

Verify `targets` matches measurement order. Existing 0.1 history files without
`targets` must still load.

Export:

```sh
./bin/pace export --format jsonl --output /tmp/pace.jsonl
./bin/pace export --format csv --output /tmp/pace.csv
```

JSONL contains one run per line. CSV contains one row per measurement; a run with
five targets produces five data rows.

Permission checks:

```sh
stat -f '%Sp %N' "$HOME/Library/Application Support/pace/history/v1"
stat -f '%Sp %N' "$HOME/Library/Application Support/pace/history/v1/"*.json
```

Expected:

```text
directory: drwx------
files:     -rw-------
```

## 12. Homebrew gate

On supported macOS arm64 versions:

```sh
brew style Formula/pace.rb
brew audit --strict --new --formula soundadam/tap/pace
HOMEBREW_NO_INSTALL_FROM_API=1 brew install --build-from-source soundadam/tap/pace
brew test soundadam/tap/pace
pace version
pace doctor --json
```

Formula tests remain offline and do not invoke the selector, station probes, or
real bandwidth measurements.
