// Package main provides the entry point for the feature-atlas service.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/JoobyPM/feature-atlas-service/internal/httpapi"
	"github.com/JoobyPM/feature-atlas-service/internal/store"
)

func main() {
	var (
		listen    = flag.String("listen", ":8443", "listen address")
		tlsCert   = flag.String("tls-cert", "certs/server.crt", "server cert")
		tlsKey    = flag.String("tls-key", "certs/server.key", "server key")
		clientCA  = flag.String("client-ca", "certs/ca.crt", "client CA (root)")
		adminCert = flag.String("admin-cert", "certs/admin.crt", "admin client cert (used to bootstrap admin role)")
		seedCount = flag.Int("seed", 200, "seed feature count")
	)
	flag.Parse()

	st := store.New()
	st.SeedFeatures(*seedCount)

	// Bootstrap admin client from certificate file
	adminFP, err := fingerprintFromCertFile(*adminCert)
	if err != nil {
		log.Fatalf("read admin cert: %v", err)
	}
	st.UpsertClient(store.Client{
		Fingerprint: adminFP,
		Name:        "admin",
		Role:        store.RoleAdmin,
		CreatedAt:   time.Now(),
	})
	log.Printf("bootstrapped admin client with fingerprint: %s", adminFP)

	// Build TLS config for mTLS
	caPEM, err := os.ReadFile(*clientCA)
	if err != nil {
		log.Fatalf("read client-ca: %v", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		log.Fatalf("failed to parse client-ca PEM")
	}

	tlsConfig := &tls.Config{
		// ClientAuth determines server policy for TLS client auth.
		// RequireAndVerifyClientCert requires a valid client cert signed by ClientCAs.
		ClientAuth: tls.RequireAndVerifyClientCert,
		// ClientCAs are roots used to verify client certs per ClientAuth policy.
		ClientCAs:  caPool,
		MinVersion: tls.VersionTLS12,
	}

	s := &httpapi.Server{Store: st}
	handler := s.Routes()

	// Enforce admin-only on /admin routes (inner middleware)
	adminCheckedHandler := httpapi.AdminOnly(handler)

	// Apply mTLS middleware globally (outer - runs first, populates context)
	finalHandler := httpapi.MTLS(st, adminCheckedHandler)

	srv := &http.Server{
		Addr:         *listen,
		Handler:      finalHandler,
		TLSConfig:    tlsConfig,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("starting feature-atlas service on https://localhost%s", *listen)
	log.Printf("mTLS enabled: client certificates required")
	log.Fatal(srv.ListenAndServeTLS(*tlsCert, *tlsKey))
}

// fingerprintFromCertFile reads a PEM certificate file and returns its SHA-256 fingerprint.
func fingerprintFromCertFile(path string) (string, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("not a certificate PEM: %s", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	return store.FingerprintSHA256(cert), nil
}
