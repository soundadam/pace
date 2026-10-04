# Third-party components

The maintained LibreSpeed helper source is committed under `components` with
its own license boundary. Helper binaries are built separately and invoked
through process interfaces. Other helper sources remain pinned by immutable
version and SHA-256. Go library dependencies are locked by `go.mod` and
`go.sum`.

## LibreSpeed CLI

- Project: <https://github.com/librespeed/speedtest-cli>
- Upstream version: `v1.0.13`
- Maintained helper version: `v1.0.13-campus.1`
- Commit: `2f2408764d88e9601aa64a03b340f8e3151003e4`
- Source archive SHA-256:
  `5ad938b61e3edc0ca95e2ccff0c06e97a69383f3cbb0243bd47b21b9865f9f55`
- License: GNU Lesser General Public License v3.0
- Installed executable: `libexec/pace/librespeed-cli`

The complete maintained source is in `components/librespeed-cli`. It adds
structured progress and explicit loopback SOCKS5 support while preserving the
upstream module and LGPL license. The helper remains a separate executable and
is not statically linked into the MIT executable. The source release contains
the complete corresponding source, and the Homebrew Formula installs its
license.

## M-Lab ndt7-client-go

- Project: <https://github.com/m-lab/ndt7-client-go>
- Version: `v0.10.1`
- Commit: `4a5f6325d1d586ab38afb84566a5781b5d6c3d9a`
- Source archive SHA-256:
  `31b40268bd7a9d31bdb5507b7ade2fad2efb8abb9e7339d2f59e9cdee5340bef`
- License: Apache License 2.0
- Installed executable: `libexec/pace/ndt7-client`

pace consumes the helper's JSON event stream and identifies the client as
`pace`. Because the upstream CLI has no version-reporting command, packaging
installs `ndt7-client.version` containing `v0.10.1`; runtime preflight verifies
that sidecar before a test. The Homebrew Formula installs the upstream license.

## Apple networkQuality

Apple's `/usr/bin/networkQuality` is an operating-system-provided executable on
macOS. pace does not redistribute it, link against it, or accept terms on
the user's behalf. Product documentation links to Apple's public NetworkQuality
explanation.

## Ookla Speedtest CLI

The official Ookla `speedtest` CLI is an optional user-installed executable. It
is not bundled, downloaded, or a Homebrew Formula dependency of pace.
pace validates the executable identity and refuses the unrelated Python
`speedtest-cli`. Ookla licensing and any first-run license/GDPR acceptance remain
between the user and Ookla; pace never passes acceptance flags
automatically. In an explicit interactive command, pace may display the
official Homebrew installation commands after a preflight failure, but executes
them only after Enter and never runs uninstall commands. See
<https://www.speedtest.net/apps/cli> for official distribution and terms.

## Bubble Tea

- Project: <https://github.com/charmbracelet/bubbletea>
- Module: `charm.land/bubbletea/v2`
- Version: `v2.0.6`
- License: MIT
- Integration: linked Go library for fixed-block inline terminal rendering

Bubble Tea and its transitive Go dependencies are recorded in `go.mod` and
`go.sum`. The renderer is disabled for redirected output and global JSON mode.

## Release verification

Before each release:

1. run `go mod verify`;
2. rebuild both helper archives from their recorded SHA-256 sources;
3. run the complete offline fixture and race gates;
4. verify the Formula installs the upstream helper license files;
5. review dependency changes in `go.mod`, `go.sum`, and this notice.
