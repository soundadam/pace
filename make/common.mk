BINARY := bin/pace
GO ?= go
GOTOOLCHAIN ?= auto

.PHONY: build tools verify-mod test test-race test-campus-provider test-mlab-provider \
	test-campus-fixture test-run-fixture test-offline vet check ci package clean clean-tools
