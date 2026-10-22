VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
LDFLAGS := -s -w -X github.com/wutlu/boltcache/appinfo.Version=$(VERSION)
CONFIG  ?= config.yaml

.PHONY: help build run run-dev run-prod test test-race fuzz bench lint vet \
        generate-config validate-config show-config client benchmark test-features \
        docker docker-run clean

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

build: ## Build the boltcache binary
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o boltcache .

run: ## Run the server with config.yaml
	go run . server --config $(CONFIG)

run-dev: ## Run with config-dev.yaml (auth on, token dev-token-123)
	go run . server --config config-dev.yaml

run-prod: ## Run with config-prod.yaml (needs BOLTCACHE_TOKEN)
	go run . server --config config-prod.yaml

test: ## Run unit and integration tests
	go test -count=1 ./...

test-race: ## Run tests with the race detector
	go test -race -count=1 ./...

fuzz: ## Fuzz the protocol parser and glob matcher
	go test -run '^$$' -fuzz FuzzParseCommand -fuzztime 60s ./internal/resp
	go test -run '^$$' -fuzz FuzzMatch -fuzztime 30s ./internal/glob

bench: ## Run micro benchmarks
	go test -run '^$$' -bench . -benchmem ./internal/...

vet: ## go vet
	go vet ./...

lint: ## Run golangci-lint
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...

generate-config: ## Write a documented default config.yaml
	go run . config generate --config $(CONFIG)

validate-config: ## Validate the configuration
	go run . config validate --config $(CONFIG)

show-config: ## Print the effective configuration
	go run . config show --config $(CONFIG)

client: ## Interactive client (RESP port)
	go run . client interactive --addr localhost:6382

benchmark: ## Load test a running server
	go run . client benchmark --addr localhost:6382 --clients 50 --requests 500000 --pipeline 16

test-features: ## Smoke test a running server
	go run . client test --addr localhost:6382

docker: ## Build the Docker image
	docker build --build-arg VERSION=$(VERSION) -t boltcache:$(VERSION) -t boltcache:latest .

docker-run: ## Run the Docker image
	docker run --rm -p 6380:6380 -p 6382:6382 -p 8090:8090 -v boltcache-data:/data boltcache:latest

clean: ## Remove build output and local data
	rm -rf boltcache dist data logs
