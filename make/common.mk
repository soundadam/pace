BINARY := bin/soundprobe
GO ?= go
GOTOOLCHAIN ?= auto

# components/librespeed-cli is a nested module, so the root `go build ./...`,
# `go vet ./...` and `go test ./...` never reach it. Every module-wide gate has
# to name it explicitly.
COMPONENT_DIR := components/librespeed-cli

# Single source of truth for the pinned analysis tools. .github/workflows/ci.yml
# installs them with `make tools-vuln` / `make tools-lint` instead of repeating
# the versions, so a local run and a CI run cannot analyse with different
# scanners.
GOVULNCHECK_VERSION ?= v1.8.0
GOLANGCI_LINT_VERSION ?= v2.13.2

# Coverage floors. Measured 2026-09-20 with `make test-cover`: root module
# 79.0%, components/librespeed-cli 83.5%. The floors sit ~3 points below the
# measured values so ordinary churn does not fail the build; they are a ratchet
# against regression, not a target. Raise them deliberately after re-measuring.
COVERAGE_MIN ?= 76.0
COMPONENT_COVERAGE_MIN ?= 80.0

.PHONY: build tools tools-vuln tools-lint verify-mod test test-race \
	test-campus-provider test-mlab-provider test-campus-fixture test-run-fixture \
	test-homebrew-template test-release-artifact test-offline test-vuln \
	test-cover test-cover-root test-cover-component vet fmt-check check-versions \
	component-check lint lint-strict check ci-checks ci release snapshot \
	release-check clean clean-tools
