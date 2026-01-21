.PHONY: all build run test clean certs fmt check lint help

# Build variables
BINARY_NAME := feature-atlasd
BUILD_DIR := ./bin
CMD_DIR := ./cmd/feature-atlasd
GO := go

# Default target
all: fmt check build

# Build the binary
build:
	@echo "==> Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "    Binary: $(BUILD_DIR)/$(BINARY_NAME)"

# Run the service (requires certs to be generated first)
run: build certs
	@echo "==> Starting $(BINARY_NAME)..."
	$(BUILD_DIR)/$(BINARY_NAME) \
		-listen :8443 \
		-tls-cert certs/server.crt \
		-tls-key certs/server.key \
		-client-ca certs/ca.crt \
		-admin-cert certs/admin.crt \
		-seed 200

# Run tests
test:
	@echo "==> Running tests..."
	$(GO) test -v -race ./...

# Generate certificates
certs:
	@echo "==> Generating certificates..."
	@chmod +x ./scripts/gen-certs.sh
	@./scripts/gen-certs.sh ./certs

# Format code
fmt:
	@echo "==> Formatting code..."
	$(GO) fmt ./...
	@if command -v goimports > /dev/null 2>&1; then \
		goimports -w .; \
	fi

# Run all checks (lint, vet, etc.)
check: lint vet

# Run linter
lint:
	@echo "==> Running linter..."
	@if command -v golangci-lint > /dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "    golangci-lint not installed, skipping..."; \
	fi

# Run go vet
vet:
	@echo "==> Running go vet..."
	$(GO) vet ./...

# Clean build artifacts
clean:
	@echo "==> Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -rf certs/

# Download dependencies
deps:
	@echo "==> Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

# Register alice client (requires service to be running)
register-alice:
	@echo "==> Registering alice client..."
	@CERT_PEM=$$(cat certs/alice.crt | sed 's/$$/\\n/' | tr -d '\n') && \
	curl -s --cacert certs/ca.crt \
		--cert certs/admin.crt --key certs/admin.key \
		-H "Content-Type: application/json" \
		-d "{\"name\":\"alice\",\"role\":\"user\",\"cert_pem\":\"$$CERT_PEM\"}" \
		https://localhost:8443/admin/v1/clients | jq .

# Test the /me endpoint as alice (requires alice to be registered)
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

# Test feature search as admin
test-search:
	@echo "==> Testing feature search..."
	@curl -s --cacert certs/ca.crt \
		--cert certs/admin.crt --key certs/admin.key \
		"https://localhost:8443/api/v1/features?query=&limit=5" | jq .

# Show help
help:
	@echo "feature-atlas-service Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all             Format, check, and build (default)"
	@echo "  build           Build the binary"
	@echo "  run             Build and run the service"
	@echo "  test            Run tests"
	@echo "  certs           Generate TLS certificates"
	@echo "  fmt             Format code"
	@echo "  check           Run linter and vet"
	@echo "  lint            Run golangci-lint"
	@echo "  vet             Run go vet"
	@echo "  clean           Remove build artifacts and certs"
	@echo "  deps            Download dependencies"
	@echo "  register-alice  Register alice client (service must be running)"
	@echo "  test-me-alice   Test /me endpoint as alice"
	@echo "  test-me-admin   Test /me endpoint as admin"
	@echo "  test-search     Test feature search"
	@echo "  help            Show this help"
