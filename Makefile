.PHONY: all build build-cli run test clean certs fmt lint check help \
        docker-build docker-run docker-stop docker-logs \
        register-alice test-me-alice test-me-admin test-search \
        test-integration test-e2e test-all

# Build variables
SERVICE_NAME := feature-atlasd
CLI_NAME := featctl
BUILD_DIR := ./bin
GO := go

# Docker variables
DOCKER_IMAGE := feature-atlas-service
DOCKER_TAG := latest

# golangci-lint v2 requires explicit config
GOLANGCI_LINT := golangci-lint
GOLANGCI_CONFIG := .golangci.yml

# Default target
all: fmt check build

# ============================================================================
# Build Targets
# ============================================================================

# Build the service binary
build:
	@echo "==> Building $(SERVICE_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(SERVICE_NAME) ./cmd/feature-atlasd
	@echo "    Binary: $(BUILD_DIR)/$(SERVICE_NAME)"

# Build the CLI binary
build-cli:
	@echo "==> Building $(CLI_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(CLI_NAME) ./cmd/featctl
	@echo "    Binary: $(BUILD_DIR)/$(CLI_NAME)"

# Build all binaries
build-all: build build-cli

# Run the service (requires certs to be generated first)
run: build certs
	@echo "==> Starting $(SERVICE_NAME)..."
	$(BUILD_DIR)/$(SERVICE_NAME) \
		-listen :8443 \
		-tls-cert certs/server.crt \
		-tls-key certs/server.key \
		-client-ca certs/ca.crt \
		-admin-cert certs/admin.crt \
		-seed 200

# ============================================================================
# Code Quality (golangci-lint v2)
# ============================================================================

# Format code using golangci-lint v2 formatters (gofumpt + gci)
fmt:
	@echo "==> Formatting code (golangci-lint fmt)..."
	@if command -v $(GOLANGCI_LINT) > /dev/null 2>&1; then \
		$(GOLANGCI_LINT) fmt --config $(GOLANGCI_CONFIG) ./...; \
	else \
		echo "    golangci-lint not installed, falling back to go fmt..."; \
		$(GO) fmt ./...; \
	fi

# Run linter using golangci-lint v2
lint:
	@echo "==> Running linter (golangci-lint run)..."
	@if command -v $(GOLANGCI_LINT) > /dev/null 2>&1; then \
		$(GOLANGCI_LINT) run --config $(GOLANGCI_CONFIG) ./...; \
	else \
		echo "    ERROR: golangci-lint not installed"; \
		echo "    Install: https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	fi

# Run go vet
vet:
	@echo "==> Running go vet..."
	$(GO) vet ./...

# Run all checks (lint includes vet via govet linter)
check: lint

# ============================================================================
# Testing
# ============================================================================

# Run tests
test:
	@echo "==> Running tests..."
	$(GO) test -v -race ./...

# Run tests with coverage
test-cover:
	@echo "==> Running tests with coverage..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "    Coverage report: coverage.html"

# Run integration tests (requires Docker)
test-integration: docker-build
	@echo "==> Running integration tests..."
	$(GO) test -v -count=1 -tags=integration ./test/integration/...

# Run e2e tests (requires Docker)
test-e2e: docker-build build-cli
	@echo "==> Running e2e tests..."
	FEATCTL_PATH=$(shell pwd)/bin/featctl $(GO) test -v -count=1 -tags=e2e ./test/e2e/...

# Run all tests (unit + integration + e2e)
test-all: test test-integration test-e2e

# ============================================================================
# Certificates
# ============================================================================

# Generate certificates
certs:
	@echo "==> Generating certificates..."
	@chmod +x ./scripts/gen-certs.sh
	@./scripts/gen-certs.sh ./certs

# ============================================================================
# Docker
# ============================================================================

# Build Docker image
docker-build:
	@echo "==> Building Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

# Run with Docker Compose
docker-run: certs
	@echo "==> Starting with Docker Compose..."
	docker compose up -d

# Stop Docker Compose
docker-stop:
	@echo "==> Stopping Docker Compose..."
	docker compose down

# View Docker logs
docker-logs:
	docker compose logs -f

# ============================================================================
# Dependencies
# ============================================================================

# Download dependencies
deps:
	@echo "==> Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# ============================================================================
# Cleanup
# ============================================================================

# Clean build artifacts
clean:
	@echo "==> Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -rf certs/
	rm -f coverage.out coverage.html

# ============================================================================
# API Testing (requires service to be running)
# ============================================================================

# Register alice client
register-alice:
	@echo "==> Registering alice client..."
	@CERT_PEM=$$(awk '{printf "%s\\n", $$0}' certs/alice.crt) && \
	curl -s --cacert certs/ca.crt \
		--cert certs/admin.crt --key certs/admin.key \
		-H "Content-Type: application/json" \
		-d "{\"name\":\"alice\",\"role\":\"user\",\"cert_pem\":\"$$CERT_PEM\"}" \
		https://localhost:8443/admin/v1/clients | jq .

# Test the /me endpoint as alice
test-me-alice:
	@echo "==> Testing /me as alice..."
	@curl -s --cacert certs/ca.crt \
		--cert certs/alice.crt --key certs/alice.key \
		https://localhost:8443/api/v1/me | jq .

# Test the /me endpoint as admin
test-me-admin:
	@echo "==> Testing /me as admin..."
	@curl -s --cacert certs/ca.crt \
		--cert certs/admin.crt --key certs/admin.key \
		https://localhost:8443/api/v1/me | jq .

# Test feature search
test-search:
	@echo "==> Testing feature search..."
	@curl -s --cacert certs/ca.crt \
		--cert certs/admin.crt --key certs/admin.key \
		"https://localhost:8443/api/v1/features?query=&limit=5" | jq .

# ============================================================================
# Help
# ============================================================================

help:
	@echo "feature-atlas-service Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Build:"
	@echo "  all             Format, check, and build (default)"
	@echo "  build           Build the service binary"
	@echo "  build-cli       Build the CLI binary"
	@echo "  build-all       Build all binaries"
	@echo "  run             Build and run the service"
	@echo ""
	@echo "Code Quality (golangci-lint v2):"
	@echo "  fmt             Format code (gofumpt + gci)"
	@echo "  lint            Run linter"
	@echo "  check           Run all checks (alias for lint)"
	@echo "  vet             Run go vet"
	@echo ""
	@echo "Testing:"
	@echo "  test            Run unit tests"
	@echo "  test-cover      Run unit tests with coverage report"
	@echo "  test-integration Run integration tests (requires Docker)"
	@echo "  test-e2e        Run e2e tests (requires Docker)"
	@echo "  test-all        Run all tests (unit + integration + e2e)"
	@echo "  certs           Generate TLS certificates"
	@echo ""
	@echo "Docker:"
	@echo "  docker-build    Build Docker image"
	@echo "  docker-run      Start with Docker Compose"
	@echo "  docker-stop     Stop Docker Compose"
	@echo "  docker-logs     View Docker logs"
	@echo ""
	@echo "Other:"
	@echo "  deps            Download dependencies"
	@echo "  clean           Remove build artifacts and certs"
	@echo "  help            Show this help"
	@echo ""
	@echo "API Testing (requires running service):"
	@echo "  register-alice  Register alice client"
	@echo "  test-me-alice   Test /me as alice"
	@echo "  test-me-admin   Test /me as admin"
	@echo "  test-search     Test feature search"
