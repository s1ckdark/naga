# clusterctl Makefile

# Variables
BINARY_NAME=clusterctl
SERVER_BINARY=clusterctl-server
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOVET=$(GOCMD) vet
GOFMT=gofmt

# Build directories
BUILD_DIR=./build
CMD_CLI=./cmd/clusterctl
CMD_SERVER=./cmd/server

.PHONY: all build build-cli build-server clean test lint fmt vet deps run-server help

# Default target
all: deps build

## Build targets

build: build-cli build-server ## Build all binaries

build-cli: ## Build CLI binary
	@echo "Building CLI..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_CLI)

build-server: ## Build server binary
	@echo "Building server..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVER_BINARY) $(CMD_SERVER)

## Development targets

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	$(GOMOD) tidy
	$(GOMOD) download

run-server: ## Run server locally
	$(GOCMD) run $(CMD_SERVER)/main.go

run-cli: ## Run CLI locally
	$(GOCMD) run $(CMD_CLI)/main.go

## Test targets

test: ## Run tests
	@echo "Running tests..."
	$(GOTEST) -v ./...

test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

## Code quality targets

lint: ## Run linter (requires golangci-lint)
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

fmt: ## Format code
	@echo "Formatting code..."
	$(GOFMT) -s -w .

vet: ## Run go vet
	@echo "Running go vet..."
	$(GOVET) ./...

check: fmt vet lint test ## Run all checks

## Installation targets

install: build-cli ## Install CLI to GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/

## Clean targets

clean: ## Remove build artifacts
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

## Database targets

db-init: ## Initialize database with migrations
	@echo "Initializing database..."
	@mkdir -p ~/.clusterctl
	sqlite3 ~/.clusterctl/clusterctl.db < migrations/001_init.sql

## Docker targets

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t clusterctl:$(VERSION) .

docker-run: ## Run Docker container
	docker run -it --rm \
		-v ~/.clusterctl:/root/.clusterctl \
		-p 8080:8080 \
		clusterctl:$(VERSION)

## Help

help: ## Show this help
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'
