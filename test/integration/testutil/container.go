package testutil

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// DefaultImage is the Docker image for the feature-atlas service.
	DefaultImage = "feature-atlas-service:latest"

	// containerAPIPort is the mTLS API port inside the container.
	containerAPIPort = "8443/tcp"

	// containerHealthPort is the health check port inside the container.
	containerHealthPort = "8080/tcp"
)

// ServerContainer wraps a testcontainers container running feature-atlasd.
type ServerContainer struct {
	Container  testcontainers.Container
	APIPort    int    // Host port mapped to container's 8443
	HealthPort int    // Host port mapped to container's 8080
	Host       string // Usually "localhost"
}

// ServerContainerConfig configures the server container.
type ServerContainerConfig struct {
	// Image is the Docker image to use. Defaults to DefaultImage.
	Image string

	// Certs is the certificate bundle to mount into the container.
	Certs *CertBundle

	// SeedCount is the number of features to seed. Defaults to 10.
	SeedCount int
}

// StartServerContainer starts a feature-atlasd container for testing.
func StartServerContainer(ctx context.Context, cfg ServerContainerConfig) (*ServerContainer, error) {
	if cfg.Image == "" {
		cfg.Image = DefaultImage
	}
	if cfg.SeedCount == 0 {
		cfg.SeedCount = 10
	}
	if cfg.Certs == nil {
		return nil, errors.New("certs bundle is required")
	}

	req := testcontainers.ContainerRequest{
		Image:        cfg.Image,
		ExposedPorts: []string{containerAPIPort, containerHealthPort},
		Cmd: []string{
			"-listen", ":8443",
			"-health-port", ":8080",
			"-tls-cert", "/certs/server.crt",
			"-tls-key", "/certs/server.key",
			"-client-ca", "/certs/ca.crt",
			"-admin-cert", "/certs/admin.crt",
			"-seed", strconv.Itoa(cfg.SeedCount),
		},
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      cfg.Certs.CACertPath,
				ContainerFilePath: "/certs/ca.crt",
				FileMode:          0o644,
			},
			{
				HostFilePath:      cfg.Certs.ServerCertPath,
				ContainerFilePath: "/certs/server.crt",
				FileMode:          0o644,
			},
			{
				HostFilePath:      cfg.Certs.ServerKeyPath,
				ContainerFilePath: "/certs/server.key",
				FileMode:          0o644, // Relaxed for test container (runs as non-root)
			},
			{
				HostFilePath:      cfg.Certs.AdminCertPath,
				ContainerFilePath: "/certs/admin.crt",
				FileMode:          0o644,
			},
		},
		WaitingFor: wait.ForHTTP("/healthz").
			WithPort(containerHealthPort).
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	// Get mapped ports
	apiPort, err := container.MappedPort(ctx, containerAPIPort)
	if err != nil {
		terminateContainer(ctx, container)
		return nil, fmt.Errorf("get API port: %w", err)
	}

	healthPort, err := container.MappedPort(ctx, containerHealthPort)
	if err != nil {
		terminateContainer(ctx, container)
		return nil, fmt.Errorf("get health port: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		terminateContainer(ctx, container)
		return nil, fmt.Errorf("get host: %w", err)
	}

	return &ServerContainer{
		Container:  container,
		APIPort:    apiPort.Int(),
		HealthPort: healthPort.Int(),
		Host:       host,
	}, nil
}

// terminateContainer terminates a container, ignoring errors (used in error paths).
func terminateContainer(ctx context.Context, container testcontainers.Container) {
	//nolint:errcheck // cleanup on error path, best effort
	container.Terminate(ctx)
}

// APIURL returns the full URL for the mTLS API.
func (sc *ServerContainer) APIURL() string {
	return fmt.Sprintf("https://%s:%d", sc.Host, sc.APIPort)
}

// HealthURL returns the full URL for the health endpoint.
func (sc *ServerContainer) HealthURL() string {
	return fmt.Sprintf("http://%s:%d", sc.Host, sc.HealthPort)
}

// Terminate stops and removes the container.
func (sc *ServerContainer) Terminate(ctx context.Context) error {
	if sc.Container != nil {
		return sc.Container.Terminate(ctx)
	}
	return nil
}

// TestEnv bundles all resources needed for an integration test.
type TestEnv struct {
	Certs  *CertBundle
	Server *ServerContainer
}

// SetupTestEnv creates a complete test environment with certs and running server.
func SetupTestEnv(ctx context.Context) (*TestEnv, error) {
	certs, err := GenerateCerts()
	if err != nil {
		return nil, fmt.Errorf("generate certs: %w", err)
	}

	server, err := StartServerContainer(ctx, ServerContainerConfig{
		Certs:     certs,
		SeedCount: 10,
	})
	if err != nil {
		certs.Cleanup()
		return nil, fmt.Errorf("start server: %w", err)
	}

	return &TestEnv{
		Certs:  certs,
		Server: server,
	}, nil
}

// Cleanup terminates the server and removes certificates.
func (te *TestEnv) Cleanup(ctx context.Context) {
	if te.Server != nil {
		//nolint:errcheck // cleanup function, best effort
		te.Server.Terminate(ctx)
	}
	if te.Certs != nil {
		te.Certs.Cleanup()
	}
}
