build:
	go build -o boltcache main-config.go server.go config.go rest.go main.go lua.go

run:
	go mod download && go run main-config.go server.go config.go rest.go main.go lua.go

run-dev:
	go mod download && go run main-config.go server.go config.go rest.go main.go lua.go -config config-dev.yaml

run-prod:
	go mod download && go run main-config.go server.go config.go rest.go main.go lua.go -config config-prod.yaml

generate-config:
	go run main-config.go server.go config.go rest.go main.go lua.go -generate-config

validate-config:
	go run main-config.go server.go config.go rest.go main.go lua.go -validate -config config.yaml

cluster-master:
	go run cluster.go cluster node1 6380

cluster-slave:
	go run cluster.go cluster node2 6381 :6380

client:
	go run client.go interactive

benchmark:
	go run client.go benchmark

test-features:
	go run client.go test

test-rest:
	chmod +x examples/rest-examples.sh && ./examples/rest-examples.sh

test-auth:
	chmod +x examples/auth-examples.sh && ./examples/auth-examples.sh

test-pubsub:
	chmod +x examples/test-pubsub.sh && ./examples/test-pubsub.sh

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
	@echo "Then open: http://localhost:8090/rest-client.html"

test:
	go test ./...

clean:
	rm -f boltcache
	rm -rf data/

docker-build:
	docker build -t boltcache .

docker-run:
	docker run -p 6380:6380 -v $(PWD)/data:/app/data boltcache

.PHONY: build run run-dev run-prod generate-config validate-config config-help show-config cluster-master cluster-slave client benchmark test-features test-rest web-client test clean docker-build docker-run