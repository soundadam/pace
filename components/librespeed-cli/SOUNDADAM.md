# Maintained LibreSpeed CLI component

This directory is a source-level derivative of LibreSpeed CLI v1.0.13 and is
licensed under the adjacent GNU LGPL v3 license.

## Provenance

- Upstream: <https://github.com/librespeed/speedtest-cli>
- Upstream tag: `v1.0.13`
- Upstream commit: `2f2408764d88e9601aa64a03b340f8e3151003e4`
- Upstream source archive SHA-256:
  `5ad938b61e3edc0ca95e2ccff0c06e97a69383f3cbb0243bd47b21b9865f9f55`
- Maintained component version: `v1.0.13-campus.1`

## Local changes

- `--progress-json` emits structured progress events on stderr while preserving
  the final JSON document on stdout.
- `--proxy socks5h://LOOPBACK:PORT` uses an explicit loopback SOCKS5 proxy,
  resolves the measurement host through that proxy, and supports cancellation.
- Ambient HTTP proxy variables are ignored for measurement traffic so reported
  route labels remain accurate.

## `--progress-json` wire contract

soundprobe's campus runner parses this stream to drive its live display, so the
framing below is a hard dependency. It decodes with unknown fields disallowed:
adding, renaming, removing or retyping any field kills the helper mid-measurement
with no compile error anywhere. `defs/progress_json_test.go` enforces the shape.

Requires `--json`; without it the helper exits with
`--progress-json requires --json`. Without `--progress-json`, nothing is written.

Newline-delimited JSON on **stderr**; the final JSON document still goes to
stdout. One compact object per line, terminated by a single `\n`, with no
embedded newline and no surrounding whitespace:

```json
{"type":"progress","test":"download","elapsed_ms":401,"bytes":5242880,"mbps":104.6}
```

Exactly these five fields, always present, with no others:

- `type`: always `"progress"`. There is no completion, summary or error event —
  the stream simply stops, and failures are reported as plain text on stderr.
- `test`: `"download"` or `"upload"`.
- `elapsed_ms`: whole milliseconds since this phase's counter started, always at
  least 1. Sub-millisecond samples are skipped rather than emitted as 0, because
  `Duration.Milliseconds` truncates and the consumer rejects a non-positive value.
- `bytes`: cumulative bytes transferred in this phase.
- `mbps`: `bytes / seconds / 125000`, i.e. decimal megabits per second. The
  divisor is fixed, so `--mebibytes` does **not** affect it — unlike the final
  report's rates, which switch to 131072.

Samples are emitted every 200 ms. Within a phase `elapsed_ms` and `bytes` are
non-decreasing; every `download` event precedes every `upload` event; and
`elapsed_ms` restarts for the upload phase, so it is not a run-global clock.

Keep modifications in this directory self-contained and covered by LGPL. A
consumer may choose only the capabilities it needs; the helper binary and source
remain independently buildable from the proprietary soundconnect application.
