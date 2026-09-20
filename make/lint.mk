# golangci-lint, report only.
#
# Deliberately not part of `ci-checks` and not a required CI gate: the owner
# wants to see what it reports before deciding whether to act on it. Both
# targets print findings and always exit 0, so they cannot fail a build by
# accident. .golangci.yml holds the starting set; `lint-strict` layers a
# broader set on top without changing the committed configuration.
LINT_STRICT_LINTERS := revive,bodyclose,errorlint,nilerr,prealloc

lint:
	GO=$(GO) GOTOOLCHAIN=$(GOTOOLCHAIN) GOLANGCI_LINT_VERSION=$(GOLANGCI_LINT_VERSION) \
		./scripts/run-golangci-lint.sh . $(COMPONENT_DIR)

lint-strict:
	GO=$(GO) GOTOOLCHAIN=$(GOTOOLCHAIN) GOLANGCI_LINT_VERSION=$(GOLANGCI_LINT_VERSION) \
		GOLANGCI_LINT_EXTRA_LINTERS=$(LINT_STRICT_LINTERS) \
		./scripts/run-golangci-lint.sh . $(COMPONENT_DIR)

tools-vuln:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

# golangci-lint must be BUILT with a Go at least as new as the toolchain that
# compiles the code it analyses, otherwise every file fails to type-check with
# "could not load export data: export data version N is greater than maximum
# supported version M". Installing from source here guarantees that; a
# distro/Homebrew golangci-lint on PATH can be older and will produce exactly
# that error. If you see it, run `make tools-lint`.
tools-lint:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
