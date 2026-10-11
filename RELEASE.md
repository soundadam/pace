# Releasing pace

Artifact names, the Cask and the update contract follow
[appkit](https://github.com/soundadam/appkit). Push a `v<version>` tag and
[the release workflow](.github/workflows/release.yml) does the rest:

```text
git tag v0.6.0 && git push origin v0.6.0
        │
        ▼
.github/workflows/release.yml
  ├─ package: scripts/package.sh <version> builds darwin/linux/windows ×
  │           amd64/arm64 (CGO_ENABLED=0, -X main.version=<version>) into
  │           pace-<version>-<os>-<arch>.tar.gz|zip, and checks that the
  │           Linux binary reports the tag's version
  ├─ release: gh release create with the archives attached
  │           (a tag containing "-" becomes a GitHub prerelease)
  └─ homebrew: fills packaging/homebrew/pace.rb and pushes Casks/pace.rb
              to soundadam/homebrew-tap (skipped for prereleases)
```

`main.version` comes from the tag; the code carries no version of its own.

## Principles

- **Never release from a dirty worktree.** The tag must point at a commit
  that passed automated and operator acceptance.
- **CI never runs a real bandwidth measurement.** `make test-offline` and
  all packaging tests use local fixtures only.
- **Real measurements are operator acceptance**, performed manually from
  [TESTING.md](TESTING.md) before tagging.
- **Releases are immutable.** The repository has release immutability on:
  once published, neither the assets nor the tag can change. Ship a new
  version instead.
- **Helper licensing stays explicit.** The archives contain only the MIT
  pace binary plus `LICENSE`, `README.md`, and
  `THIRD_PARTY_NOTICES.md`. The LibreSpeed helper (LGPL-3.0-only) and ndt7
  helper (Apache-2.0) are never bundled or auto-downloaded; the cask
  caveats say so.

## 1. One-time setup

- A public `soundadam/homebrew-tap` repository must exist.
- Configure the `HOMEBREW_TAP_GITHUB_TOKEN` secret in
  `soundadam/pace` (Settings > Secrets and variables > Actions): a
  fine-grained PAT with "Contents: read and write" on
  `soundadam/homebrew-tap`. Details are commented in
  [.github/workflows/release.yml](.github/workflows/release.yml).
- The cask was renamed from `soundprobe` to `pace` in v0.5.0. The tap keeps
  `"soundprobe": "pace"` in `cask_renames.json` (and no longer ships
  `Casks/soundprobe.rb`) so `brew upgrade` moves existing installs to
  `pace`.

## 2. Before tagging

On a supported macOS machine:

```sh
make test-offline
GOTOOLCHAIN=auto go test -race ./...
make build
./bin/pace doctor --json
```

Perform the real operator tests from [TESTING.md](TESTING.md) (NJU IPv4 /
IPv6, M-Lab after consent, combined run, partial failure, Ctrl-C and
history readback).

Optionally rehearse the release locally; this builds the same archives into
`dist/` and publishes nothing:

```sh
make package VERSION=0.6.0
```

Finally confirm the worktree is clean:

```sh
git diff --check
go mod verify
git status --short
```

## 3. Tag and push

For version `0.6.0`:

```sh
git switch main
git pull --ff-only
git tag -a v0.6.0 -m "pace v0.6.0"
git push origin v0.6.0
```

A signed tag is preferred. Pushing the tag is the release; everything after
this point is automated.

## 4. Verify the release

When the `Release` workflow finishes:

- the GitHub Release page and asset URLs return HTTP 200 anonymously, and
  each asset shows its SHA-256 digest (`pace update` verifies against it);
- `soundadam/homebrew-tap` received a commit updating
  `Casks/pace.rb` with the new version and sha256.

Then, on macOS:

```sh
brew update
brew install --cask soundadam/tap/pace
pace version
pace doctor
```

Cask-level checks are offline; a real measurement afterwards is optional
operator verification, never CI.

A directly downloaded older release updates itself with `pace update`.

Prerelease tags (e.g. `v0.6.0-rc.1`) create a GitHub prerelease and skip the
tap update; `pace update` never offers them.

## 5. Source Formula

`make release VERSION=0.6.0` runs
[scripts/build-release.sh](scripts/build-release.sh) to produce a
deterministic source `dist/pace-0.6.0.tar.gz` and renders the
source-build Formula from
[packaging/homebrew/pace.rb.tmpl](packaging/homebrew/pace.rb.tmpl)
via
[scripts/render-homebrew-formula.sh](scripts/render-homebrew-formula.sh).
That Formula builds pace and both helpers from source and carries
`license all_of: ["MIT", "LGPL-3.0-only", "Apache-2.0"]`; its `test do`
block is offline. The tap does not ship it.
`scripts/test-homebrew-template.sh` and
`scripts/test-release-artifact.sh` keep this path exercised in
`make test-offline`.

- [ ] Decide whether the source Formula stays: appkit's channels are the
  release archives and the Cask only, and a Formula-installed pace is
  detected as a plain executable, so `pace update` would replace the
  binary inside the Cellar.

## 6. `homebrew/core` consideration

Do not advertise `brew install pace` from core until Homebrew accepts
the Formula. A future submission should demonstrate:

- a public, stable, immutable release;
- an actively maintained upstream project;
- successful builds and tests across Homebrew's supported matrix;
- checksummed and locked source dependencies;
- meaningful offline Formula tests;
- no runtime downloads or self-updates;
- clear licensing for pace and both installed helper executables.

Until then, the supported installation path is through
`soundadam/homebrew-tap`.
