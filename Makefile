.PHONY: help build test test-integration lint clean

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the workload-exporter binary
	go build -o workload-exporter

test: ## Run unit tests
	go test -v ./...

test-integration: ## Run integration tests against multiple CockroachDB versions
	@echo "Running integration tests..."
	@echo "Note: This will download CockroachDB binaries (cached after first run)"
	@echo "This may take several minutes..."
	go test -tags=integration -v -timeout=20m ./pkg/export/

lint: ## Run linter
	golangci-lint run --timeout=5m

clean: ## Clean build artifacts
	rm -f workload-exporter
	go clean -testcache
