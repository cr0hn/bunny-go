GOLANGCI_LINT ?= $(or $(wildcard .bin/golangci-lint),golangci-lint)

.PHONY: test test-integration lint check

test:
	go test -race -count=1 ./...

# Opt-in: needs the BUNNY_* variables described in CONTRIBUTING.md.
test-integration:
	go test -race -count=1 -tags integration ./...

lint:
	$(GOLANGCI_LINT) run ./...

check: lint test
	go vet ./...
