BINARY := shelfpress
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/tobeck/shelfpress/internal/cli.version=$(VERSION)

COMPOSE := docker compose -f deploy/dev/docker-compose.yml
BOOKSTACK_DEV_URL := http://localhost:8080

.PHONY: help build test test-integration lint fmt tidy-check check clean \
	dev-up dev-down dev-clean dev-logs

help: ## List the available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

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

dev-up: ## Start the local BookStack and wait until its API answers
	$(COMPOSE) up -d --wait
	@echo
	@echo "BookStack is up at $(BOOKSTACK_DEV_URL)"
	@echo "Sign in with admin@admin.com / password — see deploy/dev/README.md"

dev-down: ## Stop the local BookStack, keeping its database
	$(COMPOSE) down

dev-clean: ## Stop the local BookStack and delete its data
	$(COMPOSE) down --volumes

dev-logs: ## Follow the local BookStack logs
	$(COMPOSE) logs -f
