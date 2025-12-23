build:
	go build -o boltcache .

# Run server
run:
	go run . server --config config.yaml

run-dev:
	go run . server --config config-dev.yaml

run-prod:
	go run . server --config config-prod.yaml

# Config management
generate-config:
	go run . config generate

validate-config:
	go run . config validate --config config.yaml


# Cluster nodes
cluster-master:
	go run . cluster --node node1 --port 6380

cluster-slave:
	go run . cluster --node node2 --port 6381 --replica :6380


# Client commands
client:
	go run . client interactive --addr :6380

benchmark:
	go run . client benchmark --addr :6380

test-features:
	go run . client test --addr :6380


# Testing scripts

# OLD TEST COMMAND
# test-rest:
# 	chmod +x examples/rest-examples.sh && ./examples/rest-examples.sh

test-rest:
	go test ./internal/server -run TestREST

test-auth:
	chmod +x examples/auth-examples.sh && ./examples/auth-examples.sh

# test-pubsub:
# 	chmod +x examples/test-pubsub.sh && ./examples/test-pubsub.sh
test-pubsub:
	go test ./internal/server -run TestRESTPubSub


# Helpers
config-help:
	@echo "Configuration Commands:"
	@echo "  make generate-config  - Generate default config.yaml"
	@echo "  make validate-config  - Validate configuration"
	@echo "  make run-dev         - Run with development config"
	@echo "  make run-prod        - Run with production config"
	@echo "  make show-config     - Show current configuration"
	@echo "  make test-auth       - Test authentication"

show-config:
	cat config.yaml

web-client:
	@echo "Web client available at: http://localhost:8090/rest-client.html"
	@echo "Make sure server is running with: make run-dev"

# Tests & Cleanup
test:
	go test ./...

clean:
	rm -f boltcache
	rm -rf data/

# Phony targets
.PHONY: build run run-dev run-prod generate-config validate-config cluster-master cluster-slave client benchmark test-features test-rest test-auth test-pubsub config-help show-config web-client test clean
