# Define the Go binary and output directory
GO ?= go
OUTPUT_DIR ?= ./bin
PROJECT_NAME ?= gitlab-sync
MAIN_FILE ?= .
DOCKERFILE ?= Containerfile
DOCKER_ENGINE ?= podman
GO_BUILD_FLAGS ?= -buildvcs=true

# Default target
.DEFAULT_GOAL := build

# Versions of the Go tools installed on demand by the targets below.
# Kept as annotated variables so Renovate bumps them (see renovate.json customManagers).
# renovate: datasource=go depName=gotest.tools/gotestsum
gotestsum_version := v1.13.0
# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2
golangci_lint_version := v2.13.0
# renovate: datasource=go depName=golang.org/x/vuln
govulncheck_version := v1.7.0

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download

# Build target
build: deps
	@echo "Building the binary..."
	$(GO) build $(GO_BUILD_FLAGS) -o $(OUTPUT_DIR)/$(PROJECT_NAME) $(MAIN_FILE)

# Lint target
lint: deps
	@command -v golangci-lint >/dev/null 2>&1 || { echo "Installing golangci-lint..."; go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(golangci_lint_version); }
	@echo "Running golangci-lint..."
	golangci-lint run ./...

dependency-check:
	@echo "Running dependency-check..."
	dependency-check --nvdApiKey $(NVD_API_KEY) --scan ./ --format ALL --out dependency-check/ --enableExperimental

# Test target
test: deps
	@command -v gotestsum >/dev/null 2>&1 || { echo "Installing gotestsum..."; go install gotest.tools/gotestsum@$(gotestsum_version); }
	@mkdir -p codequality
	gotestsum --junitfile codequality/unit-tests.xml --format-icons octicons -- -coverprofile=codequality/coverage.out -covermode=atomic ./...
	@echo "Coverage report generated: codequality/coverage.html"

# Scan dependencies and stdlib for known vulnerabilities (govulncheck).
vuln: deps
	@command -v govulncheck >/dev/null 2>&1 || { echo "Installing govulncheck..."; go install golang.org/x/vuln/cmd/govulncheck@$(govulncheck_version); }
	govulncheck ./...

# Docker target
package:
	@echo "Building Docker image..."
	$(DOCKER_ENGINE) build -t $(PROJECT_NAME):dev -f $(DOCKERFILE) .

# Phony targets
.PHONY: deps build lint dependency-check test package
