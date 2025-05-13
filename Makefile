.PHONY: build test clean mock lint fmt run docker-build docker-run help

# Variables
BINARY_NAME=drupdater
DOCKER_IMAGE=ghcr.io/drupdater/drupdater-php8.3
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.version=${VERSION}"

# Default target
.DEFAULT_GOAL := help

# Main targets
build: ## Build the binary
	go build ${LDFLAGS} -o ${BINARY_NAME} .

test: ## Run tests
	go test -v ./...

clean: ## Clean build artifacts
	rm -f ${BINARY_NAME}
	go clean

mock: ## Generate mocks
	docker run -v "$${PWD}":/src -w /src -e GOFLAGS="-buildvcs=false" vektra/mockery:3.2

lint: ## Run linters
	go vet ./...
	staticcheck ./...
	golangci-lint run ./...
	docker run --rm -i hadolint/hadolint < Dockerfile

fmt: ## Format code
	go fmt ./...

run: ## Run the application (requires REPO and TOKEN args)
	@if [ -z "$(REPO)" ] || [ -z "$(TOKEN)" ]; then \
		echo "Usage: make run REPO=<repository_url> TOKEN=<your_token> [OPTIONS=--flag1 --flag2]"; \
		exit 1; \
	fi
	go run ${LDFLAGS} main.go $(REPO) $(TOKEN) $(OPTIONS)

docker-build: ## Build Docker image
	docker build -t ${DOCKER_IMAGE}:latest .
	docker tag ${DOCKER_IMAGE}:latest ${DOCKER_IMAGE}:${VERSION}

docker-run: ## Run Docker image (requires REPO and TOKEN args)
	@if [ -z "$(REPO)" ] || [ -z "$(TOKEN)" ]; then \
		echo "Usage: make docker-run REPO=<repository_url> TOKEN=<your_token> [OPTIONS=--flag1 --flag2]"; \
		exit 1; \
	fi
	docker run ${DOCKER_IMAGE}:latest $(REPO) $(TOKEN) $(OPTIONS)

update: ## Update dependencies
	go get -u ./...
	go mod tidy

# Help target
help: ## Display this help
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
