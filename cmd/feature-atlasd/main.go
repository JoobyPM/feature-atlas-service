// Package main provides the entry point for the feature-atlas service.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JoobyPM/feature-atlas-service/internal/httpapi"
	"github.com/JoobyPM/feature-atlas-service/internal/store"
)

func main() {
	var (
		listen     = flag.String("listen", ":8443", "HTTPS listen address (mTLS)")
		healthPort = flag.String("health-port", ":8080", "HTTP health check port (no auth)")
		tlsCert    = flag.String("tls-cert", "certs/server.crt", "server cert")
		tlsKey     = flag.String("tls-key", "certs/server.key", "server key")
		clientCA   = flag.String("client-ca", "certs/ca.crt", "client CA (root)")
		adminCert  = flag.String("admin-cert", "certs/admin.crt", "admin client cert (used to bootstrap admin role)")
		seedCount  = flag.Int("seed", 200, "seed feature count")
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

	// Main API server (mTLS required)
	apiHandler := s.Routes()
	adminCheckedHandler := httpapi.AdminOnly(apiHandler)
	finalHandler := httpapi.MTLS(st, adminCheckedHandler)

	apiServer := &http.Server{
		Addr:         *listen,
		Handler:      finalHandler,
		TLSConfig:    tlsConfig,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Health check server (no auth, plain HTTP)
	healthServer := &http.Server{
		Addr:         *healthPort,
		Handler:      s.HealthRoutes(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Channel to receive errors from servers
	errChan := make(chan error, 2)

	// Start health server
	go func() {
		log.Printf("health endpoints on http://localhost%s (/healthz, /readyz)", *healthPort)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- fmt.Errorf("health server: %w", err)
		}
	}()

	// Start API server
	go func() {
		log.Printf("starting feature-atlas service on https://localhost%s", *listen)
		log.Printf("mTLS enabled: client certificates required")
		if err := apiServer.ListenAndServeTLS(*tlsCert, *tlsKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- fmt.Errorf("api server: %w", err)
		}
	}()

	// Wait for interrupt signal or server error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		log.Fatalf("server error: %v", err)
	case sig := <-sigChan:
		log.Printf("received signal %v, shutting down...", sig)
	}

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown both servers
	if err := apiServer.Shutdown(ctx); err != nil {
		log.Printf("api server shutdown error: %v", err)
	}
	if err := healthServer.Shutdown(ctx); err != nil {
		log.Printf("health server shutdown error: %v", err)
	}

	log.Println("shutdown complete")
}

// fingerprintFromCertFile reads a PEM certificate file and returns its SHA-256 fingerprint.
func fingerprintFromCertFile(path string) (string, error) {
	//nolint:gosec // path is from trusted command-line flag
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
