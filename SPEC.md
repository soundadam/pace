# pace v0.4 implementation specification

## 1. Product contract

pace measures explicitly selected network targets. A target represents one
measurement purpose and, where relevant, one address family. Results from
separate targets must never be silently substituted, ranked, or collapsed into a
synthetic score.

The maintained targets are:

| Station ID | Measurement purpose | Families | Method |
| --- | --- | --- | --- |
| `nju-campus` | current path to NJU's campus-internal service | IPv4, IPv6 | LibreSpeed, three concurrent streams |
| `mlab` | general Internet bulk-transport performance and current egress reference | automatic | M-Lab NDT7, single stream |
| `apple` | macOS public throughput and responsiveness under load | automatic | Apple `networkQuality`, single invocation |
| `ookla` | nearby operator-side reference | automatic | official Ookla Speedtest CLI, dynamic server |
| `cernet` | CERNET public station | IPv4 | LibreSpeed, three concurrent streams |
| `tongji` | Tongji University station | IPv4 | LibreSpeed, three concurrent streams |

IPv4 and IPv6 are independent measurements for supported stations; a dual plan
expands them into ordered targets. Retired station IDs (`nju-edge`, `qlu`) are
not catalogued and must not appear in the selector, setup, or `stations` list.
Historical JSON provider IDs from those stations remain readable in schema-v1
history.

The product is a cross-platform Go CLI for macOS, Linux, and Windows. Apple
`networkQuality` is a macOS-only optional provider; the core campus, M-Lab, and
Ookla paths do not require macOS APIs. An unavailable optional provider is
removed from a combined `run` plan before execution; an explicit provider
command fails closed with a diagnostic. The product has no daemon, privileged
helper, account, cloud synchronization, automatic scheduler, or dependency on
soundVPN/SFM/NJUConnect.

## 2. Target identities

Stable JSON provider IDs encode station and family:

```text
nju-campus-ipv4
nju-campus-ipv6
mlab
apple
ookla
cernet-ipv4
tongji-ipv4
```

The legacy provider IDs `campus`, `nju-edge-ipv4`, `nju-edge-ipv6`, and
`qlu-ipv4` remain valid only so schema-v1 history can still be read. New
measurements use the maintained IDs above.

Every new run summary includes an ordered `targets` array. The number and order
of measurement objects must match the requested targets. Duplicate targets are
rejected or deduplicated before execution, not executed twice accidentally.

## 3. LibreSpeed target behavior

Use the maintained LibreSpeed CLI source in `components/librespeed-cli`, based
on pinned upstream v1.0.13. Keep every supported server definition in the
release and pass one selected definition through `--local-json -`. Do not fetch
an uncontrolled remote server directory during routine execution.

Run the helper with the equivalent of:

```text
--local-json -
--server 1
--duration 10
--concurrent 3
--no-icmp
--telemetry-level disabled
--json
--ipv4 | --ipv6
```

Do not pass `--share`. Validate the pinned URL scheme and expected hostname
before execution. Parse exactly one final JSON result and preserve server/client
metadata, ping, jitter, upload/download rates, byte counts, duration,
concurrency, and helper version.

The maintained helper exposes a machine-readable live-rate stream through
`--progress-json`. Interactive output may display those observed rates but must
not fabricate samples or percentages.

### 3.1 NJU Campus

Pinned endpoints:

```text
IPv4  http://speed.nju.edu.cn
IPv6  http://speed6.nju.edu.cn
```

The service describes the path to NJU's campus-internal measurement servers.
`campus` defaults to IPv4. `campus --ipv6` runs IPv6 only, with no IPv4
fallback.

### 3.2 Domestic stations

Pinned IPv4 endpoints:

```text
CERNET  http://speedtest.sec.edu.cn
Tongji  https://dev.tongji.edu.cn/speedtest
```

These are optional independent targets. One station failure must not prevent
later selected stations from running. The default `domestic` command runs
Tongji. CERNET remains available only through an explicit target argument while
its backend is unreachable.

## 4. M-Lab behavior

Run pinned `ndt7-client` v0.10.1 with JSON events, TLS verification, client name
`pace`, both download and upload, and a 55-second whole-test timeout. Use
M-Lab Locate rather than pinning a server.

Consume `starting`, `connected`, `measurement`, `error`, and `complete` events.
Measurement events may drive transient download/upload rates in the interactive
view. Persist only the durable final summary, selected server, client identity,
bytes, duration, and final rates.

M-Lab is a peer target, not a fallback or reference score. Its single-stream
NDT7 result is not directly equivalent to three-stream LibreSpeed results.

## 4.1 Apple networkQuality

On macOS run `/usr/bin/networkQuality -c -s`; bind the active interface with `-I`
when the network snapshot provides one. Parse the JSON result without treating
the interface name as a server hostname. Store throughput converted from bits per
second to Mbps, base RTT, overall/directional responsiveness RPM, flow count and
the system helper version. A helper error is an attempted measurement with zero
speeds and `failure.stage=helper`; incomplete success JSON is invalid output.

Apple is a single automatic provider. It is not expanded into separate IPv4 and
IPv6 tests. If the helper exposes a usable address family it is recorded as
`ipFamily`; otherwise the active network context remains the source of interface
information.

## 4.2 Ookla Speedtest CLI

Only the official Ookla `speedtest` executable is accepted. Preflight must inspect
`speedtest --version`, require an Ookla/Speedtest identity, and reject the Python
`speedtest-cli` output. When no explicit path is configured, inspect all PATH
candidates and use the first validated official executable; `PACE_OOKLA_PATH`
is an explicit override. Run `speedtest --format=json`, adding
`--interface=<active-interface>` when available. Never pass license or GDPR
acceptance flags automatically and never bundle or maintain a replacement
implementation of the helper.

When an explicit interactive `pace ookla` command finds a missing or
conflicting helper, the CLI may offer the official Homebrew sequence
(`brew tap teamookla/speedtest`, `brew update`, `brew install speedtest --force`).
The sequence runs only after an Enter confirmation, uses direct argument arrays
instead of a shell, and never uninstalls an existing formula. Combined runs,
JSON mode, and redirected input only report the unavailable optional provider.

Parse server ID, sponsor, name/location, host, actual server address, external IP,
address family, latency, jitter, byte counts and upload/download bandwidth. The
server sponsor identifies the test server operator; it is not a claim about the
user's access carrier. Ookla is one automatic provider, not an IPv4/IPv6-expanded
pair, and is excluded from default daily stations until a user actively selects it.

The durable measurement model may include `serverId`, `serverSponsor`,
`responsivenessRpm`, `uploadResponsivenessRpm`, and
`downloadResponsivenessRpm`. These fields are optional and must remain null/absent
when a helper does not return them.

## 5. Planning and ordering

All selected targets run sequentially so they never compete for bandwidth. The
ordered plan is visible before and during execution.

### 5.1 Interactive selector

Bare `pace` in an interactive terminal performs bounded, lightweight
reachability probes and opens a Bubble Tea inline selector. Probes may check DNS,
connection establishment, TLS, and a small backend response; they must not run a
bandwidth test.

Controls:

```text
↑/↓ or j/k   move
Space        toggle station
4            IPv4
6            IPv6
d            dual stack
a            restore recommendation
Enter        execute
q / Esc      cancel
```

The selector chrome follows Teaway's Charm TUI: indigo title `pace`,
fuchsia cursor (`>`), green check (`✓`) for selected stations, faint bullet
(`•`) for unselected, and a dashed (`–`) disabled marker. Help is sparse
key names. IPv4-only stations are disabled in IPv6 mode.

Recommendation rules:

1. Select NJU Campus when at least one requested family is reachable.
2. Select M-Lab and Apple as automatic public references.
3. If Campus is not reachable, keep M-Lab and Apple rather than silently
   substituting another station.
4. Ookla is never recommended automatically; it requires an active user choice.
5. Tongji is available but not preselected. CERNET is retained only as an
   explicit compatibility target while its backend is unreachable.

Recommendations set defaults only. They do not authorize silent fallback during
measurement.

### 5.2 Noninteractive plans

JSON, redirected, and explicitly scripted commands never open the selector.
They resolve a deterministic target list from command defaults and flags:

```text
pace run --targets LIST --family ipv4|ipv6|dual
pace domestic --targets LIST --family ipv4|dual
pace campus [--ipv4|--ipv6]
pace mlab
pace apple
pace ookla
```

`--targets` accepts comma-separated station IDs. Invalid station IDs and
unsupported family/station combinations fail before any provider is contacted.

## 6. Result semantics

For each requested target:

- success stores measured values;
- attempted provider failure stores zero or partial measured values and a stable
  failure object;
- cancellation stores null speeds and a cancelled failure;
- targets not started after cancellation are marked skipped with null speeds.

Optional measurement metadata includes `serverId`, `serverSponsor`,
`responsivenessRpm`, `uploadResponsivenessRpm`, and
`downloadResponsivenessRpm`; omitted helper fields remain null/absent. An
automatic provider is executed once per plan, never once per address family.

Run status:

- all requested measurements successful: `success`;
- at least one success and at least one failure: `partial`;
- no successful measurement: `failed`;
- operator cancellation: `cancelled`.

Stable exit codes:

```text
0    all requested measurements succeeded
1    invalid configuration, missing helper, consent, or internal failure
2    failed target or partial result
130  cancelled by the operator
```

## 7. Terminal interface

Use Bubble Tea v2 in inline mode, never alternate-screen mode. Visual chrome
matches Teaway: indigo `pace` title, fuchsia accent, faint help, and a
thin `├─●─┤` activity track instead of a dense block bar. The selector must
clear before measurement progress begins. During execution redraw one fixed
block at no more than four frames per second.

Every target receives the same four-row panel:

1. explicit station/family label and phase;
2. animated activity track;
3. download/upload rates;
4. selected server or bounded failure detail.

Targets have independent waiting, active, complete, failed, cancelled, and
skipped states. M-Lab may add transient live rates; LibreSpeed targets do not.
At completion restore the cursor and replace the block with one durable summary
table using human-readable target labels.

JSON output emits exactly one document. Redirected plain output emits only one
final summary and provider failures. Neither mode may contain ANSI sequences or
raw provider events.

## 8. Commands

```text
pace
pace run [--targets LIST] [--family ipv4|ipv6|dual] [--label TEXT] [--note TEXT] [--no-save]
pace campus [--ipv4|--ipv6] [--label TEXT] [--note TEXT] [--no-save]
pace domestic [--targets LIST] [--family ipv4|dual] [--label TEXT] [--note TEXT] [--no-save]
pace mlab [--label TEXT] [--note TEXT] [--no-save]
pace stations [--json]
pace history [--limit N]
pace last [--json]
pace show RUN_ID [--json]
pace export --format jsonl|csv --output PATH
pace consent status|accept|revoke
pace doctor [--json]
pace version
```

## 9. Consent and privacy

Before a selected plan containing M-Lab starts, explain that M-Lab collects the
ISP-provided public IP address and measurement results and publishes/retains
experiment data indefinitely. Require exact interactive acceptance and store the
policy version and timestamp locally.

A plan without M-Lab never requires M-Lab consent. Noninteractive execution
without current consent fails before contacting M-Lab. LibreSpeed telemetry and
sharing are always disabled.

pace has no own analytics, remote result service, geolocation enrichment, or
ASN lookup.

## 10. Storage and export

Store summaries under the current user's platform configuration directory:

```text
macOS:   ~/Library/Application Support/pace/history/v1/<run-id>.json
Linux:   ${XDG_CONFIG_HOME:-~/.config}/pace/history/v1/<run-id>.json
Windows: %AppData%\\pace\\history\\v1\\<run-id>.json
```

Directories are `0700`; files are `0600`. Use same-directory temporary files,
fsync, and atomic rename. Never prune history automatically.

Schema version remains 1. Existing 0.1 history without a `targets` field must
remain readable. New summaries include the ordered target IDs.

JSONL export writes one complete summary per line. CSV export writes one row per
measurement, repeating run metadata. This normalized form preserves arbitrary
multi-station and dual-stack plans.

## 11. Helper discovery and packaging

Resolve helpers in this order:

1. installed `../libexec/pace` relative to the executable;
2. repository-local `.tools/bin`;
3. documented developer PATH fallback.

Apple `networkQuality` is an OS-provided optional helper at
`/usr/bin/networkQuality` and is never bundled. Ookla is an optional user-owned
helper discovered as `speedtest` on PATH; only the official Ookla identity is
accepted. `doctor --json` reports `campus` and `mlab` under required
`providers`, and Apple/Ookla independently under `optionalProviders`. Missing
optional helpers do not make base readiness false.

Verify exact helper versions and include them in results. Production must not
silently select an arbitrary newer PATH helper when a pinned libexec helper
exists.

Homebrew Formula tests are offline and may invoke only version, diagnostics, and
read-only history commands. They must not probe real stations or run bandwidth
measurements.

## 12. Verification gates

Automated tests use mock helpers and local HTTP fixtures. They cover:

- explicit station/family expansion and ordering;
- selector recommendation, family switching, toggling, cancellation, and clear;
- NJU Campus identity and no silent fallback;
- domestic station identity and telemetry-disabled helper arguments;
- M-Lab live event parsing and independent failure;
- Apple `networkQuality` success/error/timeout, interface binding and RPM fields;
- Ookla official version validation, dynamic server metadata, and Python
  `speedtest-cli` rejection;
- multi-target success, partial, failure, cancellation, and skipped results;
- schema-v1 legacy history compatibility;
- normalized CSV and JSONL export;
- inline terminal rendering, cursor restoration, and no ANSI in redirected/JSON
  output;
- atomic storage and `0700`/`0600` modes;
- deterministic release artifacts and Homebrew template rendering.

Routine CI must never run a real bandwidth measurement or station probe.
Operator acceptance validates real NJU Campus, domestic stations, M-Lab
continuation, selector interaction, Homebrew installation, and upgrade on a
supported macOS host.
