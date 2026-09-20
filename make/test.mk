test:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) test ./...

test-race:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) test -race ./...

test-campus-provider:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) test -v ./internal/helper ./internal/provider ./internal/provider/campus

test-mlab-provider:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) test -v ./internal/helper ./internal/provider ./internal/provider/mlab

test-campus-fixture:
	./scripts/test-campus-fixture.sh

test-run-fixture:
	./scripts/test-run-fixture.sh

test-homebrew-template:
	./scripts/test-homebrew-template.sh

test-release-artifact:
	./scripts/test-release-artifact.sh

test-offline: check test-campus-fixture test-run-fixture test-homebrew-template test-release-artifact

# Static vulnerability analysis over both modules. The only network access is
# the Go advisory database; nothing is measured or executed.
#
# Previously this existed only in .github/workflows/ci.yml, which is how a
# reachable golang.org/x/text advisory reached main: CI would have caught it,
# but no developer could run the check before pushing.
test-vuln:
	GO=$(GO) GOTOOLCHAIN=$(GOTOOLCHAIN) GOVULNCHECK_VERSION=$(GOVULNCHECK_VERSION) \
		./scripts/run-govulncheck.sh . $(COMPONENT_DIR)

test-cover: test-cover-root test-cover-component

test-cover-root:
	GO=$(GO) GOTOOLCHAIN=$(GOTOOLCHAIN) ./scripts/run-coverage.sh . $(COVERAGE_MIN)

test-cover-component:
	GO=$(GO) GOTOOLCHAIN=$(GOTOOLCHAIN) ./scripts/run-coverage.sh $(COMPONENT_DIR) $(COMPONENT_COVERAGE_MIN)

vet:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) vet ./...

# components/ has its own module and its own gate below; this covers the
# project's own packages.
fmt-check:
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt required on:" >&2; \
		echo "$$unformatted" >&2; \
		exit 1; \
	fi

# go.mod is the single source of truth for the Go version; anything else that
# names one is checked against it.
check-versions:
	./scripts/check-go-version-pins.sh

# The nested module. It is a locally-modified LGPL-3.0 derivative that will need
# future rebases against upstream, but it is currently gofmt-clean, so hold the
# line rather than letting drift in.
component-check:
	@unformatted="$$(cd $(COMPONENT_DIR) && gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt required in $(COMPONENT_DIR):" >&2; \
		echo "$$unformatted" >&2; \
		exit 1; \
	fi
	cd $(COMPONENT_DIR) && GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) build ./...
	cd $(COMPONENT_DIR) && GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) vet ./...

check: test vet
