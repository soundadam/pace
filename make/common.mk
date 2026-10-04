BINARY := bin/pace
GO ?= go
GOTOOLCHAIN ?= auto

.PHONY: build tools verify-mod test test-race test-campus-provider test-mlab-provider \
	test-campus-fixture test-run-fixture test-homebrew-template test-release-artifact \
	test-offline vet check ci release snapshot release-check clean clean-tools
