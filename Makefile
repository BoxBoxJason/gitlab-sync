# Define the Go binary and output directory
GO ?= go
OUTPUT_DIR ?= ./bin
PROJECT_NAME ?= gitlab-sync
MAIN_FILE ?= cmd/main.go
DOCKERFILE ?= Containerfile
DOCKER_ENGINE ?= podman

# Default target
.DEFAULT_GOAL := build

# Build target
build:
	@echo "Running go mod tidy..."
	$(GO) mod tidy
	@echo "Building the binary..."
	$(GO) build -o $(OUTPUT_DIR)/$(PROJECT_NAME) $(MAIN_FILE)

# Lint target
lint:
	@echo "Running go mod tidy..."
	$(GO) mod tidy
	@echo "Running golangci-lint..."
	golangci-lint run --tests=false --fix --issues-exit-code 0
	@echo "Running gosec..."
	gosec -enable-audit -no-fail -quiet ./...

dependency-check:
	@echo "Running dependency-check..."
	dependency-check --nvdApiKey $(NVD_API_KEY) --scan ./ --format ALL --out dependency-check/ --enableExperimental

# Test target
test:
	@echo "Running tests with coverage..."
	gotestsum --format-icons octicons -- -covermode=atomic ./...

# Docker target
package:
	@echo "Building Docker image..."
	$(DOCKER_ENGINE) build -t $(PROJECT_NAME):dev -f $(DOCKERFILE) .

# Phony targets
.PHONY: build lint dependency-check test package
