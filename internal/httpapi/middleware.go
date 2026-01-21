// Package httpapi provides HTTP handlers and middleware for the feature-atlas service.
package httpapi

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/JoobyPM/feature-atlas-service/internal/store"
)

type ctxKey string

const (
	ctxClientKey ctxKey = "client"
	ctxCertKey   ctxKey = "cert"
)

// MTLS returns middleware that validates mTLS client certificates.
// It extracts the client certificate from the TLS connection and looks up
// the client in the store by fingerprint.
func MTLS(s *store.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// net/http sets Request.TLS for TLS-enabled connections.
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "mTLS required", http.StatusUnauthorized)
			return
		}
		// PeerCertificates are parsed certs sent by peer, leaf first.
		cert := r.TLS.PeerCertificates[0]
		fp := store.FingerprintSHA256(cert)

		client, ok := s.GetClient(fp)
		if !ok {
			http.Error(w, "client cert not registered", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), ctxClientKey, client)
		ctx = context.WithValue(ctx, ctxCertKey, cert)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AdminOnly returns middleware that restricts access to admin clients only.
// Must be used after MTLS middleware.
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only enforce admin check on /admin routes
		if strings.HasPrefix(r.URL.Path, "/admin/") {
			client := ClientFromContext(r.Context())
			if client.Role != store.RoleAdmin {
				http.Error(w, "admin only", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ClientFromContext extracts the authenticated client from the request context.
func ClientFromContext(ctx context.Context) store.Client {
	v := ctx.Value(ctxClientKey)
	if v == nil {
		return store.Client{}
	}
	return v.(store.Client)
}

// CertFromContext extracts the client certificate from the request context.
func CertFromContext(ctx context.Context) *x509.Certificate {
	v := ctx.Value(ctxCertKey)
	if v == nil {
		return nil
	}
	return v.(*x509.Certificate)
}

// readAllLimit reads up to max bytes from r.
func readAllLimit(r io.Reader, max int64) ([]byte, error) {
	lr := io.LimitReader(r, max)
	return io.ReadAll(lr)
}

// parseCertPEM parses a PEM-encoded certificate.
func parseCertPEM(pemText string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}
