#!/usr/bin/env bash
# gen-certs.sh - Generate a private CA, server cert, and client certs for mTLS testing.
#
# Usage: ./scripts/gen-certs.sh [output_dir]
#   output_dir: Directory to store generated certificates (default: ./certs)

set -euo pipefail

CERT_DIR="${1:-./certs}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "==> Generating certificates in: $CERT_DIR"
mkdir -p "$CERT_DIR"
cd "$CERT_DIR"

# ============================================================================
# 1) Create CA (Certificate Authority)
# ============================================================================
echo "==> Creating CA..."
if [[ ! -f ca.key ]]; then
    openssl genrsa -out ca.key 4096
fi

if [[ ! -f ca.crt ]]; then
    openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 \
        -subj "/CN=feature-atlas-demo-ca" \
        -out ca.crt
fi
echo "    CA certificate: ca.crt"

# ============================================================================
# 2) Create Server Certificate (localhost)
# ============================================================================
echo "==> Creating server certificate..."

# Create server extension config
cat > server-ext.cnf << 'EOF'
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
IP.1 = 127.0.0.1
EOF

if [[ ! -f server.key ]]; then
    openssl genrsa -out server.key 2048
fi

if [[ ! -f server.crt ]]; then
    openssl req -new -key server.key \
        -subj "/CN=localhost" \
        -out server.csr

    openssl x509 -req -in server.csr \
        -CA ca.crt -CAkey ca.key -CAcreateserial \
        -out server.crt -days 365 -sha256 \
        -extfile server-ext.cnf

    rm -f server.csr
fi
echo "    Server certificate: server.crt"

# ============================================================================
# 3) Create Client Extension Config (shared for all clients)
# ============================================================================
cat > client-ext.cnf << 'EOF'
basicConstraints = CA:FALSE
keyUsage = digitalSignature
extendedKeyUsage = clientAuth
EOF

# ============================================================================
# 4) Create Admin Client Certificate
# ============================================================================
echo "==> Creating admin client certificate..."
if [[ ! -f admin.key ]]; then
    openssl genrsa -out admin.key 2048
fi

if [[ ! -f admin.crt ]]; then
    openssl req -new -key admin.key \
        -subj "/CN=admin" \
        -out admin.csr

    openssl x509 -req -in admin.csr \
        -CA ca.crt -CAkey ca.key -CAcreateserial \
        -out admin.crt -days 365 -sha256 \
        -extfile client-ext.cnf

    rm -f admin.csr
fi
echo "    Admin certificate: admin.crt"

# ============================================================================
# 5) Create Normal User Client Certificate (alice)
# ============================================================================
echo "==> Creating alice client certificate..."
if [[ ! -f alice.key ]]; then
    openssl genrsa -out alice.key 2048
fi

if [[ ! -f alice.crt ]]; then
    openssl req -new -key alice.key \
        -subj "/CN=alice" \
        -out alice.csr

    openssl x509 -req -in alice.csr \
        -CA ca.crt -CAkey ca.key -CAcreateserial \
        -out alice.crt -days 365 -sha256 \
        -extfile client-ext.cnf

    rm -f alice.csr
fi
echo "    Alice certificate: alice.crt"

# ============================================================================
# 6) Clean up intermediate files
# ============================================================================
rm -f server-ext.cnf client-ext.cnf

# ============================================================================
# 7) Display fingerprints
# ============================================================================
echo ""
echo "==> Certificate fingerprints (SHA-256):"
echo "    CA:     $(openssl x509 -in ca.crt -noout -fingerprint -sha256 | cut -d= -f2)"
echo "    Server: $(openssl x509 -in server.crt -noout -fingerprint -sha256 | cut -d= -f2)"
echo "    Admin:  $(openssl x509 -in admin.crt -noout -fingerprint -sha256 | cut -d= -f2)"
echo "    Alice:  $(openssl x509 -in alice.crt -noout -fingerprint -sha256 | cut -d= -f2)"

echo ""
echo "==> Done! Certificates generated in: $CERT_DIR"
echo ""
echo "Files created:"
ls -la ./*.crt ./*.key 2>/dev/null | awk '{print "    " $NF}' || true
