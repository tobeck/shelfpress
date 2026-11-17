BINARY := shelfpress
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/tobeck/shelfpress/internal/cli.version=$(VERSION)

.PHONY: build test test-integration lint fmt tidy-check check clean

build: ## Build the binary into ./dist
	go build -ldflags '$(LDFLAGS)' -o dist/$(BINARY) ./cmd/shelfpress

test: ## Run unit tests with the race detector
	go test -race ./...

test-integration: ## Run integration tests against a live BookStack (needs BOOKSTACK_URL)
	go test -tags=integration ./test/integration/...

tidy-check: ## Fail if go.mod/go.sum are not tidy (as CI does)
	go mod tidy
	git diff --exit-code go.mod go.sum

lint: ## Run golangci-lint
	golangci-lint run

fmt: ## Format the source tree
	gofmt -w .

check: fmt tidy-check lint test build ## Everything CI runs

clean:
	rm -rf dist/
