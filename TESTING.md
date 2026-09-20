# Testing soundprobe

How to verify soundprobe: offline on any machine, and by hand on an acceptance
host. [SPEC.md](SPEC.md) states what the behaviour must be; this file states how
to check it. Where a step below names a contract, the wording in SPEC.md wins.

Routine tests never contact real NJU, M-Lab, or domestic bandwidth servers.
Real measurements are explicit operator acceptance steps (sections 5 onward).

## 1. Complete offline gate

```sh
make test-offline
GOTOOLCHAIN=auto go test -race ./...
```

`make test-offline` is defined in [make/test.mk](make/test.mk); it runs package
tests, `go vet`, executable-level mock-helper fixtures, Homebrew template
checks, and deterministic release-artifact tests. Nothing in it touches a real
station. It covers:

- station registry and IPv4/IPv6/dual expansion, and the ordered target plan;
- explicit provider IDs such as `nju-campus-ipv4` and `nju-edge-ipv6`, and
  rejection of the retired `campus` provider ID;
- selector recommendation, target toggling, family changes, and cancellation;
- sequential multi-target execution and skipped targets after cancellation;
- LibreSpeed arguments with telemetry disabled;
- M-Lab JSON events, transient live rates, and final summaries;
- Apple `networkQuality` JSON success/error/timeout fixtures, `-I` binding, and
  responsiveness RPM parsing;
- official Ookla JSON/server metadata, version identity, interface binding, and
  rejection of the Python `speedtest-cli`;
- success, partial, failure, timeout, malformed output, and cancellation;
- one shared failed/cancelled measurement shape across every provider;
- history reading two ways: a `history/v1` directory (and a pre-rename
  `njuprobe` one) is never enumerated, so a user holding only old runs gets an
  empty history and no warning; a schema-v1 or otherwise unreadable file that
  sits *inside* `history/v2` is skipped and named on stderr without breaking
  `history`, `last` or `export`;
- normalized one-measurement-per-row CSV export and one-run-per-line JSONL;
- Bubble Tea inline rendering and cursor restoration;
- JSON and redirected output without ANSI or provider event leakage;
- atomic storage and `0700`/`0600` modes;
- deterministic source archive and Formula generation.

Focused tests:

```sh
GOTOOLCHAIN=auto go test ./internal/target ./internal/ui ./internal/provider/...
```

### Vulnerability scan

```sh
make tools-vuln   # once, installs the pinned govulncheck
make test-vuln
```

`make test-vuln` runs govulncheck over both modules — the root module and
`components/librespeed-cli` — the same pair CI scans. The scanner version is
pinned in [make/common.mk](make/common.mk); CI installs it with `make tools-vuln`
from that same pin, so a local scan and a CI scan never use different scanners.
The only network access is the Go vulnerability database, so results can change
without any code changing. If govulncheck is missing, the target prints the
install command rather than failing obscurely.

This target exists because its absence had a cost: a reachable
`golang.org/x/text` advisory reached `main` while govulncheck lived only in the
workflow file, so CI would have caught it but no developer could check before
pushing.

### Coverage

```sh
make test-cover
```

Measures statement coverage for both modules and fails below the floors in
[make/common.mk](make/common.mk) (`COVERAGE_MIN`, `COMPONENT_COVERAGE_MIN`): 76%
for the root module and 80% for `components/librespeed-cli`, set below the
measured 79.1% and 83.5% so ordinary churn does not fail the build. The floors
are a ratchet against regression, not a target; raise them deliberately after
re-measuring. Profiles are written to `coverage.out` in each module and removed
by `make clean`.

### Lint (report only)

```sh
make tools-lint   # once, installs the pinned golangci-lint
make lint         # staticcheck, errcheck, unused, gocritic
make lint-strict  # the above plus revive, bodyclose, errorlint, nilerr, prealloc
```

Both targets print findings and always exit 0. golangci-lint is not a gate: the
CI job that runs it is `continue-on-error`, and [.golangci.yml](.golangci.yml)
exists so the findings can be read, not so the build can be blocked on them.

On Linux amd64, the reproducible pinned-toolchain gate remains:

```sh
make ci
```

`make ci` runs [scripts/run-local-ci.sh](scripts/run-local-ci.sh), which
bootstraps the exact pinned Go toolchain (checksum-verified) plus a C compiler
for the race detector, then hands off to `make ci-checks` — the same target
[.github/workflows/ci.yml](.github/workflows/ci.yml) invokes. `ci-checks` is the
single definition of the check sequence: `check-versions`, `fmt-check`,
`verify-mod`, `test-offline`, `test-race`, `component-check`, `test-cover`,
`test-vuln`, `build`. Add or remove a check there, not in the script and not in
the workflow.

CI must not run `soundprobe stations`, because that command intentionally
performs real lightweight reachability probes.

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

`doctor --json` must report `campus` and `mlab` under `providers`, and `apple`
and `ookla` under `optionalProviders`; a missing optional helper must not make
`ready` false. Helpers must resolve in the order fixed by
[SPEC.md section 11](SPEC.md#11-helper-discovery-and-packaging) — confirm a
pinned `libexec` helper is preferred over an arbitrary newer one on `PATH`.

## 3. M-Lab consent

```sh
./bin/soundprobe consent status
./bin/soundprobe consent accept
./bin/soundprobe consent status
```

Acceptance requires a terminal and the exact word `accept`. Verify the
fail-closed rule of [SPEC.md section 9](SPEC.md#9-consent-and-privacy): a
noninteractive plan containing M-Lab fails with `consent_required` and exit `1`
before contacting M-Lab, and a plan without M-Lab never asks.

## 4. Apple and Ookla acceptance

Offline tests use fake executables and never perform a real bandwidth test. On
an acceptance host, verify helper availability first. Apple is expected only on
macOS; Linux and Windows should report it as an unavailable optional provider
without failing the base run:

```sh
./bin/soundprobe doctor --json
/usr/bin/networkQuality -h  # macOS only
speedtest --version         # only when the official Ookla CLI was installed intentionally
```

Apple must run once with `-c -s` (and `-I <active-interface>` when known), and
its JSON must expose throughput, base RTT and RPM fields. Ookla must run once
with `--format=json`; inspect server ID, sponsor, host/address, latency, jitter
and address family. Check the guarantees in
[SPEC.md section 4.2](SPEC.md#42-ookla-speedtest-cli): no `--accept-license` or
`--accept-gdpr` is ever passed, a Python `speedtest-cli` binary is reported as
unavailable rather than executed, a combined `run` drops the optional target and
continues, and an explicit `soundprobe ookla` fails before measuring.

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

Verify that every registry entry has a family/status row, that M-Lab is shown as
automatic rather than as a pinned server, and that both NJU Edge families read
`unsupported`.

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
- NJU Edge never appears in the selector, because it is not a daily-eligible
  station;
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

Both commands must exit `1` before starting LibreSpeed, with an
`invalid_arguments` error reporting that NJU Edge is unavailable in terminal
mode and naming the browser URLs to use instead. Do not add automated challenge
solving to the acceptance test.

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

- explicit labels such as `NJU Campus · IPv6`;
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

Verify `targets` matches measurement order, and confirm the two history-reading
behaviours from section 1 against a real upgraded profile rather than a fixture.
`show RUN_ID` must fail for a run it cannot read, because there is nothing to
fall back to.

Export:

```sh
./bin/soundprobe export --format jsonl --output /tmp/soundprobe.jsonl
./bin/soundprobe export --format csv --output /tmp/soundprobe.csv
```

JSONL contains one run per line. CSV contains one row per measurement; a run with
five targets produces five data rows.

Permission checks, against the layout in
[SPEC.md section 10](SPEC.md#10-storage-and-export):

```sh
stat -f '%Sp %N' "$HOME/Library/Application Support/soundprobe/history/v2"
stat -f '%Sp %N' "$HOME/Library/Application Support/soundprobe/history/v2/"*.json
```

Expected:

```text
directory: drwx------
files:     -rw-------
```

## 12. Homebrew gate

The released package is a binary **cask** (see [RELEASE.md](RELEASE.md)); the
source-build Formula is the manual fallback. On supported macOS arm64 versions:

```sh
# cask, the supported path
brew update
brew install --cask soundadam/tap/soundprobe
soundprobe version
soundprobe doctor --json

# source Formula, only when exercising the manual fallback
brew style Formula/soundprobe.rb
brew audit --strict --new --formula soundadam/tap/soundprobe
HOMEBREW_NO_INSTALL_FROM_API=1 brew install --build-from-source soundadam/tap/soundprobe
brew test soundadam/tap/soundprobe
```

Both paths' package tests are bound by the offline rule in
[SPEC.md section 11](SPEC.md#11-helper-discovery-and-packaging): version,
diagnostics and read-only history commands only, never the selector, a station
probe, or a real bandwidth measurement.
