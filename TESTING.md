# Testing soundprobe

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
- explicit provider IDs such as `nju-campus-ipv4` and `nju-edge-ipv6`;
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

CI must not run `soundprobe stations` because that command intentionally performs
real lightweight reachability probes.

## 2. Build and helpers

```sh
make tools
make build
./bin/soundprobe doctor --json
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

1. installed `../libexec/soundprobe`;
2. repository-local `.tools/bin`;
3. explicit developer PATH fallback.

## 3. M-Lab consent

```sh
./bin/soundprobe consent status
./bin/soundprobe consent accept
./bin/soundprobe consent status
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
./bin/soundprobe doctor --json
/usr/bin/networkQuality -h  # macOS only
speedtest --version          # only when the official Ookla CLI was installed intentionally
```

Apple must run once with `-c -s` (and `-I <active-interface>` when known), and its
JSON must expose throughput, base RTT and RPM fields. Ookla must run once with
`--format=json`; inspect server ID, sponsor, host/address, latency, jitter and
address family. soundprobe never passes `--accept-license` or `--accept-gdpr`.
A Python `speedtest-cli` binary must be reported as unavailable rather than
executed. A combined `run` must remove that optional target and continue with
the remaining providers; an explicit `soundprobe ookla` command must fail before
starting a measurement.

For an interactive explicit `soundprobe ookla` failure, use a fake Homebrew
resolver and command runner to verify that the official tap/update/install
sequence is displayed and runs only after an empty Enter line. Verify that a
non-empty response performs no external command, that `--json` never prompts,
and that uninstall commands are displayed only as manual conflict recovery.

## 5. Station discovery and selector

Station probes are lightweight but real:

```sh
./bin/soundprobe stations
./bin/soundprobe stations --json
```

Verify that every registry entry has a family/status row and M-Lab is shown as
automatic rather than as a pinned server.

Run the selector:

```sh
./bin/soundprobe
```

Check:

- a fresh preferences path opens the daily-station setup and saves mode `0600`;
- `soundprobe setup` can change daily stations;
- a `preferences.json` from an older schema (e.g. `"schemaVersion": 1`) re-opens
  setup on the next run and is overwritten, rather than failing with
  `preferences_error` and exit 1;
- later bare runs show only the configured daily stations;
- the selector clears before progress begins;
- NJU Campus plus M-Lab and Apple are recommended; Ookla is never auto-selected;
- NJU Edge is omitted from daily CLI choices and its error points to the web test;
- `4`, `6`, and `d` switch family modes;
- IPv4-only domestic stations are disabled in IPv6 mode;
- Space toggles stations and Enter starts the exact visible order;
- q, Esc, and Ctrl-C cancel without creating a history entry.

## 6. Real NJU acceptance

These commands perform real uploads and downloads.

### Campus

```sh
./bin/soundprobe campus --ipv4 --no-save --json
./bin/soundprobe campus --ipv6 --no-save --json
```

Expected target IDs:

```text
nju-campus-ipv4
nju-campus-ipv6
```

The IPv6 result must never contain an IPv4 family or server.

### Public Edge limitation

```sh
./bin/soundprobe edge --no-save --json
./bin/soundprobe run --targets nju-edge --family dual --no-save --json
```

Both commands must exit `1` before starting LibreSpeed and report that NJU Edge
is unavailable in terminal mode because its official backend requires browser
verification. `soundprobe stations` must show both Edge families as `unsupported`.
Do not add automated challenge solving to the acceptance test.

## 7. Domestic station acceptance

Run the two default regional stations before a complete batch. CERNET remains
an explicit compatibility probe and is expected to fail cleanly while its
current backend is unreachable:

```sh
./bin/soundprobe run --targets cernet --family ipv4 --no-save --json
./bin/soundprobe run --targets qlu --family ipv4 --no-save --json
./bin/soundprobe run --targets tongji --family ipv4 --no-save --json
```

Then validate sequential batch behavior:

```sh
./bin/soundprobe domestic --no-save --json
```

Expected target order:

```json
["tongji-ipv4", "qlu-ipv4"]
```

One failed station must not stop later stations. Inspect helper arguments during
offline tests to ensure `--telemetry-level disabled` is always present.

## 8. M-Lab and mixed plans

```sh
./bin/soundprobe mlab --no-save --json
./bin/soundprobe apple --no-save --json
./bin/soundprobe run --targets nju-campus,mlab,apple --family ipv4 --no-save --json
./bin/soundprobe run --targets nju-campus,mlab,apple --family dual --no-save --json
```

M-Lab uses automatic Locate selection. During a TTY run its download and upload
rates should update within the same fixed-height panel. NJU or domestic failures
must not prevent M-Lab from running when it is later in the selected plan.

## 9. Terminal rendering

For an interactive combined plan, verify:

- explicit labels such as `NJU Edge · IPv6`;
- one equal four-row panel per target;
- fixed panel height while live M-Lab events arrive;
- observed LibreSpeed rates with no fabricated samples or percentage;
- no alternate-screen enter/leave sequences;
- one durable final summary after the live block clears;
- cursor hidden during rendering and restored at completion.

For redirected output:

```sh
./bin/soundprobe run --targets nju-campus --family dual --no-save > /tmp/plain.txt
```

`/tmp/plain.txt` must have no ANSI bytes and no raw JSON.

For JSON:

```sh
./bin/soundprobe run --targets nju-campus --family dual --no-save --json > /tmp/run.json
python3 -m json.tool /tmp/run.json
```

The file must contain exactly one JSON document.

## 10. Cancellation

Start a multi-target plan and press Ctrl-C during an active target:

```sh
./bin/soundprobe run --targets nju-campus,mlab --family dual
```

Expected exit code: `130`. The active target is cancelled, every later target is
skipped, and no later helper starts.

## 11. History and export

Save a labeled multi-target run:

```sh
./bin/soundprobe run \
  --targets nju-campus,mlab \
  --family ipv4 \
  --label daily \
  --note "home Wi-Fi"
```

Read it back:

```sh
./bin/soundprobe last --json
./bin/soundprobe history --limit 10
./bin/soundprobe show RUN_ID --json
```

Verify `targets` matches measurement order. Existing 0.1 history files without
`targets` must still load.

Export:

```sh
./bin/soundprobe export --format jsonl --output /tmp/soundprobe.jsonl
./bin/soundprobe export --format csv --output /tmp/soundprobe.csv
```

JSONL contains one run per line. CSV contains one row per measurement; a run with
five targets produces five data rows.

Permission checks:

```sh
stat -f '%Sp %N' "$HOME/Library/Application Support/soundprobe/history/v1"
stat -f '%Sp %N' "$HOME/Library/Application Support/soundprobe/history/v1/"*.json
```

Expected:

```text
directory: drwx------
files:     -rw-------
```

## 12. Homebrew gate

On supported macOS arm64 versions:

```sh
brew style Formula/soundprobe.rb
brew audit --strict --new --formula soundadam/tap/soundprobe
HOMEBREW_NO_INSTALL_FROM_API=1 brew install --build-from-source soundadam/tap/soundprobe
brew test soundadam/tap/soundprobe
soundprobe version
soundprobe doctor --json
```

Formula tests remain offline and do not invoke the selector, station probes, or
real bandwidth measurements.
