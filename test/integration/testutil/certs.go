// Package testutil provides test utilities for integration tests.
package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// CertBundle contains all generated certificates for a test suite.
type CertBundle struct {
	Dir string // Directory containing certificate files

	// File paths
	CACertPath     string
	CAKeyPath      string
	ServerCertPath string
	ServerKeyPath  string
	AdminCertPath  string
	AdminKeyPath   string
	UserCertPath   string
	UserKeyPath    string

	// Parsed certificates (for fingerprint calculation, etc.)
	CACert     *x509.Certificate
	AdminCert  *x509.Certificate
	UserCert   *x509.Certificate
	ServerCert *x509.Certificate
}

// GenerateCerts creates a complete certificate bundle for testing.
// The caller is responsible for calling Cleanup() when done.
func GenerateCerts() (*CertBundle, error) {
	dir, err := os.MkdirTemp("", "feature-atlas-test-certs-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	bundle := &CertBundle{
		Dir:            dir,
		CACertPath:     filepath.Join(dir, "ca.crt"),
		CAKeyPath:      filepath.Join(dir, "ca.key"),
		ServerCertPath: filepath.Join(dir, "server.crt"),
		ServerKeyPath:  filepath.Join(dir, "server.key"),
		AdminCertPath:  filepath.Join(dir, "admin.crt"),
		AdminKeyPath:   filepath.Join(dir, "admin.key"),
		UserCertPath:   filepath.Join(dir, "user.crt"),
		UserKeyPath:    filepath.Join(dir, "user.key"),
	}

	// Generate CA
	caKey, caCert, err := generateCA()
	if err != nil {
		cleanupDir(dir)
		return nil, fmt.Errorf("generate CA: %w", err)
	}
	bundle.CACert = caCert

	if writeErr := writeCertAndKey(bundle.CACertPath, bundle.CAKeyPath, caCert, caKey); writeErr != nil {
		cleanupDir(dir)
		return nil, fmt.Errorf("write CA: %w", writeErr)
	}

	// Generate server certificate
	serverKey, serverCert, err := generateServerCert(caKey, caCert)
	if err != nil {
		cleanupDir(dir)
		return nil, fmt.Errorf("generate server cert: %w", err)
	}
	bundle.ServerCert = serverCert

	if writeErr := writeCertAndKey(bundle.ServerCertPath, bundle.ServerKeyPath, serverCert, serverKey); writeErr != nil {
		cleanupDir(dir)
		return nil, fmt.Errorf("write server cert: %w", writeErr)
	}

	// Generate admin client certificate
	adminKey, adminCert, err := generateClientCert(caKey, caCert, "admin")
	if err != nil {
		cleanupDir(dir)
		return nil, fmt.Errorf("generate admin cert: %w", err)
	}
	bundle.AdminCert = adminCert

	if writeErr := writeCertAndKey(bundle.AdminCertPath, bundle.AdminKeyPath, adminCert, adminKey); writeErr != nil {
		cleanupDir(dir)
		return nil, fmt.Errorf("write admin cert: %w", writeErr)
	}

	// Generate normal user client certificate
	userKey, userCert, err := generateClientCert(caKey, caCert, "testuser")
	if err != nil {
		cleanupDir(dir)
		return nil, fmt.Errorf("generate user cert: %w", err)
	}
	bundle.UserCert = userCert

	if writeErr := writeCertAndKey(bundle.UserCertPath, bundle.UserKeyPath, userCert, userKey); writeErr != nil {
		cleanupDir(dir)
		return nil, fmt.Errorf("write user cert: %w", writeErr)
	}

	return bundle, nil
}

// cleanupDir removes a directory, ignoring errors (used in error paths).
func cleanupDir(dir string) {
	//nolint:errcheck // cleanup on error path, best effort
	os.RemoveAll(dir)
}

// Cleanup removes all generated certificate files.
func (b *CertBundle) Cleanup() {
	if b.Dir != "" {
		//nolint:errcheck // cleanup function, best effort
		os.RemoveAll(b.Dir)
	}
}

// generateCA creates a self-signed CA certificate.
func generateCA() (*ecdsa.PrivateKey, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "feature-atlas-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, fmt.Errorf("parse certificate: %w", err)
	}

	return key, cert, nil
}

// generateServerCert creates a server certificate signed by the CA.
func generateServerCert(caKey *ecdsa.PrivateKey, caCert *x509.Certificate) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:     []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, fmt.Errorf("parse certificate: %w", err)
	}

	return key, cert, nil
}

// generateClientCert creates a client certificate signed by the CA.
func generateClientCert(caKey *ecdsa.PrivateKey, caCert *x509.Certificate, cn string) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, fmt.Errorf("parse certificate: %w", err)
	}

	return key, cert, nil
}

// writeCertAndKey writes a certificate and key to PEM files.
func writeCertAndKey(certPath, keyPath string, cert *x509.Certificate, key *ecdsa.PrivateKey) error {
	// Write certificate
	//nolint:gosec // certPath is from internal test code
	certFile, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("create cert file: %w", err)
	}
	defer certFile.Close()

	if encodeErr := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); encodeErr != nil {
		return fmt.Errorf("encode cert: %w", encodeErr)
	}

	// Write key
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}

	//nolint:gosec // keyPath is from internal test code
	keyFile, err := os.OpenFile(keyPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create key file: %w", err)
	}
	defer keyFile.Close()

	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return fmt.Errorf("encode key: %w", err)
	}

	return nil
}
