# feature-atlas-service

A Go microservice for managing a feature catalog with **dual-mode backend support**:
- **Atlas Mode**: mTLS-authenticated server with in-memory storage
- **GitLab Mode**: Git-based feature catalog using GitLab as the backend

## Features

- **Dual Backend Support**: Switch between Atlas (mTLS server) and GitLab (Git-based catalog)
- **GitLab Integration**: OAuth2 Device Flow authentication, MR-based feature creation
- **mTLS Authentication**: Certificate-based client authentication for Atlas mode
- **Interactive TUI**: Bubble Tea-based terminal UI with autocomplete
- **Local Manifest**: Track features locally with sync to remote
- **Offline Support**: Work offline with local manifest, sync when connected
- **Docker Support**: Containerized deployment with docker-compose

## Quick Start

### Option A: GitLab Mode (Recommended for teams)

GitLab mode stores features in a GitLab repository as YAML files. Changes are made via merge requests.

**1. Create a configuration file** (`~/.config/featctl/config.yaml`):

```yaml
mode: gitlab
gitlab:
  url: https://gitlab.com
  project: your-group/feature-catalog
  main_branch: main
  mr:
    labels:
      - feature
    remove_source_branch: true
```

**2. Authenticate with GitLab**:

```bash
# Interactive OAuth2 Device Flow
./bin/featctl login

# Or use a Personal Access Token
./bin/featctl login --token <your-token>

# Or set environment variable (CI/CD)
export FEATCTL_GITLAB_TOKEN=<your-token>
```

**3. Use the CLI**:

```bash
# Search features in GitLab
./bin/featctl search "keyword"

# Browse features interactively
./bin/featctl tui

# Create a new feature (opens MR)
./bin/featctl feature create --id FT-LOCAL-my-feature --name "My Feature" --summary "Description"

# Sync local features to GitLab
./bin/featctl manifest sync
```

### Option B: Atlas Mode (mTLS Server)

Atlas mode uses a centralized server with mTLS authentication.

#### 1. Generate Certificates

```bash
make certs
```

This creates a private CA and certificates for:
- `server.crt/key` - Server certificate (localhost)
- `admin.crt/key` - Admin client certificate  
- `alice.crt/key` - Normal user client certificate

#### 2. Run the Service

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

The `featctl` CLI supports both GitLab and Atlas backends.

```bash
featctl [command] [flags]

Commands:
  # Common commands (both modes)
  search        Search features in the catalog
  get           Get a feature by ID
  tui           Interactive terminal UI for browsing features
  lint          Validate a YAML file against the feature catalog
  config show   Display current configuration

  # GitLab mode commands
  login         Authenticate with GitLab (OAuth2 or token)
  logout        Remove stored GitLab credentials
  auth status   Show authentication status

  # Manifest commands
  manifest init     Create a new local manifest
  manifest list     List features in local manifest
  manifest add      Add a server feature to local manifest
  manifest sync     Sync local features with remote
  manifest pending  Show pending merge requests (GitLab mode)

  # Feature commands
  feature create    Create a new local feature

  # Atlas mode commands
  me            Show authenticated client information

Global Flags:
  --mode    Backend mode: atlas or gitlab (default from config)

Atlas Mode Flags:
  --server  Server URL (default: https://localhost:8443)
  --ca      CA certificate file (default: certs/ca.crt)
  --cert    Client certificate file (default: certs/alice.crt)
  --key     Client private key file (default: certs/alice.key)

Manifest Sync Flags (GitLab mode):
  --dry-run       Show what would be synced without changes
  --force-local   Push local changes via MR
  --force-remote  Overwrite local changes with remote
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

### featctl Configuration

The CLI supports configuration via YAML file, environment variables, and CLI flags.

**Config file locations** (in order of precedence):
1. `--config` flag
2. `.featctl.yaml` in current or parent directories
3. `~/.config/featctl/config.yaml`

**Full configuration example**:

```yaml
# Mode: "atlas" or "gitlab"
mode: gitlab

# GitLab mode configuration
gitlab:
  url: https://gitlab.com
  project: group/feature-catalog
  main_branch: main
  # token: set via env var or login command
  mr:
    labels:
      - feature
    remove_source_branch: true
    default_assignee: username

# Atlas mode configuration
atlas:
  server: https://localhost:8443
  ca_cert: certs/ca.crt
  client_cert: certs/alice.crt
  client_key: certs/alice.key
```

**Environment variables**:

| Variable | Description |
|----------|-------------|
| `FEATCTL_MODE` | Backend mode (`atlas` or `gitlab`) |
| `FEATCTL_GITLAB_URL` | GitLab instance URL |
| `FEATCTL_GITLAB_TOKEN` | GitLab access token |
| `FEATCTL_GITLAB_PROJECT` | GitLab project path |
| `FEATCTL_ATLAS_SERVER` | Atlas server URL |

### feature-atlasd Configuration

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
