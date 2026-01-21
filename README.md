# feature-atlas-service

A Go microservice demonstrating **mutual TLS (mTLS)** authentication. This is a PoC for learning how mTLS works: the server requires a client certificate during the TLS handshake, and clients must present valid certificates signed by a trusted CA.

## What is mTLS?

In standard TLS, only the server presents a certificate. In **mutual TLS**, both sides authenticate:
- The **server** presents its certificate (just like regular HTTPS)
- The **client** also presents a certificate
- Both certificates must be signed by a trusted Certificate Authority (CA)

This provides strong authentication at the transport layer, before any application code runs.

## Features

- **mTLS required**: Server uses `tls.RequireAndVerifyClientCert`
- **In-memory storage**: Clients and features stored in memory
- **Certificate-based authorization**: Client fingerprint mapped to user/role
- **Public API**: Search, suggest, and retrieve features
- **Admin API**: Register clients, reseed feature catalog
- **Interactive TUI**: Bubble Tea-based terminal UI with autocomplete
- **Docker support**: Containerized deployment with docker-compose

## Quick Start

### 1. Generate Certificates

```bash
make certs
```

This creates a private CA and certificates for:
- `server.crt/key` - Server certificate (localhost)
- `admin.crt/key` - Admin client certificate  
- `alice.crt/key` - Normal user client certificate

### 2. Run the Service

**Option A: Run locally**

```bash
make run
```

**Option B: Run with Docker**

```bash
make docker-build
make docker-run
```

The service starts on `https://localhost:8443` with mTLS enabled.

### 3. Use the CLI

```bash
# Build the CLI
make build-cli

# Show your identity
./bin/featctl me

# Search features
./bin/featctl search "keyword"

# Interactive TUI browser
./bin/featctl tui

# Validate a YAML file
./bin/featctl lint my-feature.yaml
```

### 4. Test with curl

Test as admin (bootstrapped automatically):

```bash
curl --cacert certs/ca.crt \
  --cert certs/admin.crt --key certs/admin.key \
  https://localhost:8443/api/v1/me
```

Register alice as a user:

```bash
curl --cacert certs/ca.crt \
  --cert certs/admin.crt --key certs/admin.key \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"alice\",\"role\":\"user\",\"cert_pem\":\"$(awk '{printf "%s\\n", $0}' certs/alice.crt)\"}" \
  https://localhost:8443/admin/v1/clients
```

## CLI Reference

The `featctl` CLI uses mTLS to communicate with the service.

```bash
featctl [command] [flags]

Commands:
  me        Show authenticated client information
  search    Search features in the catalog
  get       Get a feature by ID
  tui       Interactive terminal UI for browsing features
  lint      Validate a YAML file against the feature catalog

Global Flags:
  --server  Server URL (default: https://localhost:8443)
  --ca      CA certificate file (default: certs/ca.crt)
  --cert    Client certificate file (default: certs/alice.crt)
  --key     Client private key file (default: certs/alice.key)
```

## API Reference

### Public API (requires registered client cert)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/me` | Get authenticated client info |
| GET | `/api/v1/features?query=<q>&limit=<n>` | Search features |
| GET | `/api/v1/features/<id>` | Get feature by ID |
| GET | `/api/v1/suggest?query=<q>&limit=<n>` | Autocomplete suggestions |

### Admin API (requires admin role)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/admin/v1/clients` | List registered clients |
| POST | `/admin/v1/clients` | Register a new client |
| POST | `/admin/v1/features/seed?count=<n>` | Reseed feature catalog |

## Docker Deployment

### Build and Run

```bash
# Generate certificates first
make certs

# Build Docker image
make docker-build

# Start with docker-compose
make docker-run

# View logs
make docker-logs

# Stop
make docker-stop
```

### Docker Compose Configuration

The service mounts certificates from `./certs` and exposes port 8443:

```yaml
services:
  feature-atlas:
    build: .
    ports:
      - "8443:8443"
    volumes:
      - ./certs:/app/certs:ro
```

## Architecture

```
┌─────────────┐        TLS Handshake          ┌──────────────────┐
│   Client    │ ─────────────────────────────▶│     Server       │
│  (with cert)│◀───────────────────────────── │  (verifies cert) │
└─────────────┘                               └──────────────────┘
                                                      │
                                                      ▼
                                              ┌──────────────────┐
                                              │   Middleware     │
                                              │  - Extract cert  │
                                              │  - Fingerprint   │
                                              │  - Lookup client │
                                              └──────────────────┘
                                                      │
                                                      ▼
                                              ┌──────────────────┐
                                              │   In-Memory DB   │
                                              │  - Clients       │
                                              │  - Features      │
                                              └──────────────────┘
```

## mTLS Flow

1. **TLS Handshake**: Client connects, server requests client certificate
2. **Certificate Verification**: Go's TLS library verifies the client cert is signed by the CA
3. **Fingerprint Extraction**: Middleware computes SHA-256 fingerprint of the client certificate
4. **Authorization**: Fingerprint looked up in the in-memory client database
5. **Role Check**: Admin endpoints additionally verify the client has `admin` role

## Project Structure

```
feature-atlas-service/
├── cmd/
│   ├── feature-atlasd/     # Service entry point
│   └── featctl/            # CLI entry point
├── internal/
│   ├── store/              # In-memory data store
│   ├── httpapi/            # HTTP handlers + middleware
│   ├── apiclient/          # mTLS HTTP client
│   └── tui/                # Bubble Tea TUI
├── scripts/
│   └── gen-certs.sh        # Certificate generation
├── certs/                  # Generated certificates (gitignored)
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── README.md
```

## Development

```bash
# Format code (gofumpt + gci)
make fmt

# Run linter (golangci-lint v2)
make lint

# Build all
make build-all

# Run tests with coverage
make test-cover

# Clean
make clean
```

## Configuration

Command-line flags for `feature-atlasd`:

| Flag | Default | Description |
|------|---------|-------------|
| `-listen` | `:8443` | HTTPS listen address (mTLS required) |
| `-health-port` | `:8080` | HTTP health check port (no auth) |
| `-tls-cert` | `certs/server.crt` | Server certificate |
| `-tls-key` | `certs/server.key` | Server private key |
| `-client-ca` | `certs/ca.crt` | CA for verifying client certs |
| `-admin-cert` | `certs/admin.crt` | Admin cert (bootstrapped at startup) |
| `-seed` | `200` | Number of features to seed |

### Health Endpoints (No Auth Required)

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` | Liveness probe - returns `{"status": "ok"}` |
| `GET /readyz` | Readiness probe - includes feature count check |

## Related

- [Cloudflare: What is mTLS?](https://www.cloudflare.com/learning/access-management/what-is-mutual-tls/)
- [Go crypto/tls package](https://pkg.go.dev/crypto/tls)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - Terminal UI framework
- [Cobra](https://github.com/spf13/cobra) - CLI framework

## License

MIT
