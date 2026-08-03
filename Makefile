.PHONY: build test test-race test-property fuzz mutate clean mock lint fmt fix run docker-build docker-run docs-serve docs-build help

# Variables
BINARY_NAME=drupdater
DOCKER_IMAGE=drupdater-local
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X github.com/drupdater/drupdater/internal.Version=${VERSION}"

# Default target
.DEFAULT_GOAL := help

# Main targets
build: ## Build the binary
	go build ${LDFLAGS} -o ${BINARY_NAME} .

test: ## Run tests
	go test -v ./...

# Around a minute rather than a couple of seconds: pkg/composer and pkg/drush fake subprocesses
# by re-executing the test binary, and -race instruments every one of those execs.
test-race: ## Run tests with the race detector
	go test -race ./...

# RAPID_CHECKS rather than -rapid.checks: rapid only registers its flags in test binaries that
# import it, so passing the flag to ./... fails on every package that has no property tests. The
# environment variable is read at init and simply ignored by those packages.
test-property: ## Run only the property tests, with far more generated cases than `make test`
	RAPID_CHECKS=10000 go test ./... -run TestProperty

# `go test ./...` already replays every seed and every committed counterexample; this is the
# generative run. One target at a time -- `go test -fuzz` refuses a pattern matching several.
FUZZTIME ?= 30s

fuzz: ## Fuzz every target for FUZZTIME each (default 30s)
	@set -e; \
	for pkg in $$(go list ./...); do \
	  for target in $$(go test -list '^Fuzz' $$pkg 2>/dev/null | grep '^Fuzz' || true); do \
	    echo "==> $$target ($$pkg)"; \
	    go test $$pkg -run '^$$' -fuzz "^$$target$$" -fuzztime $(FUZZTIME); \
	  done; \
	done

clean: ## Clean build artifacts
	rm -f ${BINARY_NAME}
	go clean

mock: ## Generate mocks
	docker run -v "$${PWD}":/src -w /src -e GOFLAGS="-buildvcs=false" vektra/mockery:3.7

lint: ## Run linters
	golangci-lint run ./...
	docker run --rm -i -e XDG_CONFIG_HOME=/bin -v ${PWD}/.hadolint.yaml:/bin/hadolint.yaml hadolint/hadolint < Dockerfile

deadcode: ## Find unreachable functions
	go tool deadcode -test ./...

# RAPID_NOFAILFILE stops the property tests from recording a counterexample for every mutant
# they kill. Those files are the point of a mutation run, not a finding about the real code, and
# rapid replays whatever it finds under testdata/rapid on the next ordinary test run.
mutate: ## Run mutation testing over the whole module
	RAPID_NOFAILFILE=1 go tool mutago --config mutago.yaml --coverage --quiet --no-diffs \
		--logger-summary-json ./...

fmt: ## Format code
	go fmt ./...

fix: ## Apply go fix modernizers (interface{} → any, strings.Cut, etc.)
	go fix ./...

run: ## Run the application (requires REPO and TOKEN args)
	@if [ -z "$(REPO)" ] || [ -z "$(TOKEN)" ]; then \
		echo "Usage: make run REPO=<repository_url> TOKEN=<your_token> [OPTIONS=--flag1 --flag2]"; \
		exit 1; \
	fi
	@go run ${LDFLAGS} main.go $(TOKEN) --clone --repository-url $(REPO) $(OPTIONS)

docker-build: ## Build Docker image
	docker build -t ${DOCKER_IMAGE}:latest .
	docker tag ${DOCKER_IMAGE}:latest ${DOCKER_IMAGE}:${VERSION}

docker-run: ## Run Docker image (requires REPO and TOKEN args)
	@if [ -z "$(REPO)" ] || [ -z "$(TOKEN)" ]; then \
		echo "Usage: make docker-run REPO=<repository_url> TOKEN=<your_token> [OPTIONS=--flag1 --flag2]"; \
		exit 1; \
	fi
	@docker run ${DOCKER_IMAGE}:latest $(TOKEN) --clone --repository-url $(REPO) $(OPTIONS)

update: ## Update dependencies
	go get -u ./...
	go mod tidy

docs-serve: ## Preview the documentation site at http://127.0.0.1:8000
	mkdocs serve

docs-build: ## Build the documentation site into ./site (--strict fails on broken links)
	mkdocs build --strict

# Help target
help: ## Display this help
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
