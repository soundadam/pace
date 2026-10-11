ci:
	./scripts/run-local-ci.sh

release:
	@test -n "$(VERSION)" || (echo "VERSION is required, e.g. make release VERSION=0.1.0" >&2; exit 1)
	./scripts/build-release.sh "$(VERSION)"

# The release archives for every platform, as the release workflow builds them.
package:
	@test -n "$(VERSION)" || (echo "VERSION is required, e.g. make package VERSION=0.1.0" >&2; exit 1)
	./scripts/package.sh "$(VERSION)"
