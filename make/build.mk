build:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) build -o $(BINARY) ./cmd/pace

verify-mod:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(GO) mod verify

tools:
	./scripts/install-dev-tools.sh
