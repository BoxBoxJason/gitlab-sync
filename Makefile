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
golangci_lint_version := v2.15.0
# renovate: datasource=go depName=golang.org/x/vuln
govulncheck_version := v1.9.0

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

# Regenerate the documentation pages derived from the CLI definition (docs/content/docs/cli-reference.md).
# `make test` fails when the committed page is stale.
docs-generate:
	@echo "Regenerating the generated documentation pages..."
	UPDATE_DOCS=1 $(GO) test ./cmd -run '^TestCLIReference$$' -count=1

# Build the documentation site into docs/public (needs the extended Hugo, see CONTRIBUTING.md).
docs: docs-generate
	@command -v hugo >/dev/null 2>&1 || { echo "hugo (extended) is required, see https://gohugo.io/installation/"; exit 1; }
	hugo --source docs --gc --minify

# Serve the documentation site with live reload.
docs-serve:
	@command -v hugo >/dev/null 2>&1 || { echo "hugo (extended) is required, see https://gohugo.io/installation/"; exit 1; }
	hugo server --source docs

# Docker target
package:
	@echo "Building Docker image..."
	$(DOCKER_ENGINE) build -t $(PROJECT_NAME):dev -f $(DOCKERFILE) .

# Local observability stack (Grafana + OTLP receiver) used to develop the OpenTelemetry metrics.
OTEL_COMPOSE ?= $(DOCKER_ENGINE) compose -f dev/otel/compose.yaml

otel-up:
	$(OTEL_COMPOSE) up -d
	@echo "Grafana: http://localhost:$${GRAFANA_PORT:-3030} | Prometheus: http://localhost:$${PROMETHEUS_PORT:-9091} | OTLP HTTP: http://localhost:$${OTLP_HTTP_PORT:-4318}"
	@echo "export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:$${OTLP_HTTP_PORT:-4318}"

otel-down:
	$(OTEL_COMPOSE) down

otel-logs:
	$(OTEL_COMPOSE) logs -f

# Load a week of simulated runs into the local Prometheus (see dev/otel/backfill.py --help).
otel-backfill:
	DOCKER_ENGINE=$(DOCKER_ENGINE) ./dev/otel/backfill.py $(BACKFILL_ARGS)

# Create or update the gitlab-sync dashboards (one per dev/otel/dashboards/<name>.json) in the local Grafana.
GRAFANA_URL ?= http://localhost:$${GRAFANA_PORT:-3030}
DASHBOARD_API = $(GRAFANA_URL)/apis/dashboard.grafana.app/v2/namespaces/default/dashboards

otel-dashboard:
	@for file in dev/otel/dashboards/*.json; do \
		name=$$(basename $$file .json); \
		curl -sf -o /dev/null -X PUT -H 'Content-Type: application/json' --data @$$file $(DASHBOARD_API)/$$name \
			|| curl -sf -o /dev/null -X POST -H 'Content-Type: application/json' --data @$$file $(DASHBOARD_API) \
			|| { echo "Failed to load $$file"; exit 1; }; \
		echo "Dashboard: $(GRAFANA_URL)/d/$$name"; \
	done

# Phony targets
.PHONY: deps build lint dependency-check test vuln docs-generate docs docs-serve package otel-up otel-down otel-logs otel-backfill otel-dashboard
