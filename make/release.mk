# The single definition of the check sequence.
#
# .github/workflows/ci.yml and scripts/run-local-ci.sh both invoke these
# targets rather than restating the commands, so the local script cannot drift
# away from what CI actually enforces. The two exceptions, both documented
# where they live: CI's cross-platform `build` matrix (a compile smoke test on
# macOS and Windows that a Linux script cannot reproduce), and the report-only
# `lint` job (deliberately not a gate).
ci-checks: check-versions fmt-check verify-mod test-offline test-race \
	component-check test-cover test-vuln build

ci:
	./scripts/run-local-ci.sh

release:
	@test -n "$(VERSION)" || (echo "VERSION is required, e.g. make release VERSION=0.1.0" >&2; exit 1)
	./scripts/build-release.sh "$(VERSION)"

# GoReleaser-based flow (see RELEASE.md). Requires a local goreleaser install.

# Full local dry run: builds every platform and archive into dist/ without
# publishing anything.
snapshot:
	goreleaser release --snapshot --clean

# Validate .goreleaser.yaml.
release-check:
	goreleaser check
