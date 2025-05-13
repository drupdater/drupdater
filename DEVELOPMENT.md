# Development

This document provides instructions for developers working on the Drupdater project.

## Using the Makefile

The project includes a Makefile to simplify common development tasks:

```bash
# Build the binary
make build

# Run tests
make test

# Generate mocks
make mock

# Run linters
make fmt lint

# Run the application
make run REPO=<repository_url> TOKEN=<your_token>

# Build Docker image
make docker-build

# Run using Docker
make docker-run REPO=<repository_url> TOKEN=<your_token>

# Update dependencies
make update

# Get help on available commands
make help
```
