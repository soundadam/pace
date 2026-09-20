# Releasing soundprobe

Releases are automated: push a `v*` tag, CI runs GoReleaser, and the tap
updates itself.

```text
git tag v0.3.0 && git push origin v0.3.0
        │
        ▼
.github/workflows/release.yml (GitHub Actions)
        │
        ▼
GoReleaser (.goreleaser.yaml)
  ├─ builds darwin/linux/windows × amd64/arm64 (CGO_ENABLED=0)
  ├─ archives (tar.gz, zip on Windows) + checksums.txt
  ├─ GitHub Release with a changelog grouped by feat/fix/docs
  └─ pushes Casks/soundprobe.rb to soundadam/homebrew-tap
```

Users install with:

```sh
brew install --cask soundadam/tap/soundprobe
```

## Principles (unchanged from the manual era)

- **Never release from a dirty worktree.** The tag must point at a commit
  that passed both the offline gate and operator acceptance from
  [TESTING.md](TESTING.md).
- **CI never measures.** That rule and its scope belong to
  [SPEC.md section 12](SPEC.md#12-verification-gates); packaging and
  release-artifact tests are bound by it too.
- **Release assets are immutable.** Once a cask or Formula references a
  checksum, never edit or replace the asset; ship a new version instead.
- **Helper licensing stays explicit.** The archives contain only the MIT
  soundprobe binary plus `LICENSE`, `README.md`, and
  `THIRD_PARTY_NOTICES.md`. The LibreSpeed helper (LGPL-3.0-only) and ndt7
  helper (Apache-2.0) are never bundled or auto-downloaded; the cask
  caveats say so.

## 1. One-time setup

- A public `soundadam/homebrew-tap` repository must exist.
- Configure the `HOMEBREW_TAP_GITHUB_TOKEN` secret in
  `soundadam/soundprobe` (Settings > Secrets and variables > Actions): a
  fine-grained PAT with "Contents: read and write" on
  `soundadam/homebrew-tap`. Details are commented in
  [.github/workflows/release.yml](.github/workflows/release.yml).
- If the tap previously shipped `Formula/soundprobe.rb`, add a
  `tap_migrations.json` at the tap root (`{"soundprobe": "soundprobe"}`)
  and remove the old Formula so users upgrade to the cask cleanly.

## 2. Before tagging

On a supported macOS machine, run the offline gate and the real operator
acceptance steps from [TESTING.md](TESTING.md) — sections 1 and 2 for the
gate and the build, then sections 5 through 12 for real measurement, NJU IPv4
and IPv6, M-Lab after consent, a combined run, a partial failure, Ctrl-C, and
history readback.

Optionally rehearse the release locally:

```sh
make release-check   # validate .goreleaser.yaml
make snapshot        # full multi-platform build into dist/, publishes nothing
```

Finally confirm the worktree is clean:

```sh
git diff --check
go mod verify
git status --short
```

## 3. Tag and push

For version `0.3.0`:

```sh
git switch main
git pull --ff-only
git tag -a v0.3.0 -m "soundprobe v0.3.0"
git push origin v0.3.0
```

A signed tag is preferred. Pushing the tag is the release; everything after
this point is automated.

## 4. Verify the release

When the `Release` workflow finishes:

- the GitHub Release page and asset URLs return HTTP 200 anonymously;
- `checksums.txt` matches the uploaded archives;
- `soundadam/homebrew-tap` received a commit updating
  `Casks/soundprobe.rb` with the new version and sha256.

Then, on macOS, run the cask half of
[TESTING.md section 12](TESTING.md#12-homebrew-gate). Cask-level checks are
offline; a real measurement afterwards is optional operator verification,
never CI.

Prerelease tags (e.g. `v0.4.0-rc1`) create a GitHub prerelease and skip the
tap update.

## 5. Manual fallback

If CI or GoReleaser is unavailable, the pre-automation flow still works and
is kept in-tree:

```sh
make release VERSION=0.3.0
```

This runs [scripts/build-release.sh](scripts/build-release.sh) to produce a
deterministic source `dist/soundprobe-0.3.0.tar.gz` and renders the
source-build Formula from
[packaging/homebrew/soundprobe.rb.tmpl](packaging/homebrew/soundprobe.rb.tmpl)
via
[scripts/render-homebrew-formula.sh](scripts/render-homebrew-formula.sh).
Upload the archive and its `.sha256` to the GitHub Release by hand, then
copy the Formula into the tap as `Formula/soundprobe.rb`. That Formula
builds soundprobe and both helpers from source and carries
`license all_of: ["MIT", "LGPL-3.0-only", "Apache-2.0"]`; its `test do`
block is offline. `scripts/test-homebrew-template.sh` and
`scripts/test-release-artifact.sh` keep this path exercised in
`make test-offline`.

## 6. `homebrew/core` consideration

Do not advertise `brew install soundprobe` from core until Homebrew accepts
the Formula. A future submission should demonstrate:

- a public, stable, immutable release;
- an actively maintained upstream project;
- successful builds and tests across Homebrew's supported matrix;
- checksummed and locked source dependencies;
- meaningful offline Formula tests;
- no runtime downloads or self-updates;
- clear licensing for soundprobe and both installed helper executables.

Until then, the supported installation path is through
`soundadam/homebrew-tap`.
