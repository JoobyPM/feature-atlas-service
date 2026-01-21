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

```bash
make run
```

The service starts on `https://localhost:8443` with mTLS enabled.

### 3. Test with curl

Test as admin (bootstrapped automatically):

```bash
# Get admin identity
curl --cacert certs/ca.crt \
  --cert certs/admin.crt --key certs/admin.key \
  https://localhost:8443/api/v1/me
```

Register alice as a user:

```bash
CERT_PEM="$(cat certs/alice.crt | sed 's/$/\\n/' | tr -d '\n')"

curl --cacert certs/ca.crt \
  --cert certs/admin.crt --key certs/admin.key \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"alice\",\"role\":\"user\",\"cert_pem\":\"${CERT_PEM}\"}" \
  https://localhost:8443/admin/v1/clients
```

Test as alice:

```bash
curl --cacert certs/ca.crt \
  --cert certs/alice.crt --key certs/alice.key \
  https://localhost:8443/api/v1/me
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

### Register Client Request

```json
{
  "name": "alice",
  "role": "user",
  "cert_pem": "-----BEGIN CERTIFICATE-----\n..."
}
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

## Testing mTLS

Try these scenarios to understand mTLS:

| Scenario | Expected Result |
|----------|-----------------|
| No client cert | TLS handshake fails |
| Cert signed by wrong CA | TLS handshake fails |
| Valid cert, not registered | 403 Forbidden |
| Registered user cert | Success |
| User cert on admin endpoint | 403 Forbidden |
| Admin cert on admin endpoint | Success |

```bash
# No client cert - should fail
curl --cacert certs/ca.crt https://localhost:8443/api/v1/me
# Error: SSL peer certificate or SSH remote key was not OK

# Self-signed cert - should fail
curl --cacert certs/ca.crt \
  --cert /some/other/cert.crt --key /some/other/key.key \
  https://localhost:8443/api/v1/me
# Error: TLS handshake failure
```

## Project Structure

```
feature-atlas-service/
├── cmd/
│   └── feature-atlasd/
│       └── main.go          # Entry point
├── internal/
│   ├── store/
│   │   └── store.go         # In-memory data store
│   └── httpapi/
│       ├── handlers.go      # HTTP handlers
│       └── middleware.go    # mTLS middleware
├── scripts/
│   └── gen-certs.sh         # Certificate generation
├── certs/                   # Generated certificates (gitignored)
├── Makefile
├── go.mod
└── README.md
```

## Development

```bash
# Format and lint
make fmt check

# Build
make build

# Run tests
make test

# Clean
make clean
```

## Configuration

Command-line flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-listen` | `:8443` | Listen address |
| `-tls-cert` | `certs/server.crt` | Server certificate |
| `-tls-key` | `certs/server.key` | Server private key |
| `-client-ca` | `certs/ca.crt` | CA for verifying client certs |
| `-admin-cert` | `certs/admin.crt` | Admin cert (bootstrapped at startup) |
| `-seed` | `200` | Number of features to seed |

## Related

- **feature-atlas-cli**: CLI tool that uses this service with mTLS
- [Cloudflare: What is mTLS?](https://www.cloudflare.com/learning/access-management/what-is-mutual-tls/)
- [Go crypto/tls package](https://pkg.go.dev/crypto/tls)

## License

MIT
