package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
)

// NewAdminClient creates an API client using admin credentials.
func NewAdminClient(env *TestEnv) (*apiclient.Client, error) {
	return apiclient.New(
		env.Server.APIURL(),
		env.Certs.CACertPath,
		env.Certs.AdminCertPath,
		env.Certs.AdminKeyPath,
	)
}

// NewUserClient creates an API client using normal user credentials.
func NewUserClient(env *TestEnv) (*apiclient.Client, error) {
	return apiclient.New(
		env.Server.APIURL(),
		env.Certs.CACertPath,
		env.Certs.UserCertPath,
		env.Certs.UserKeyPath,
	)
}

// RegisterUserClient registers the user certificate with the server using admin credentials.
func RegisterUserClient(ctx context.Context, adminClient *apiclient.Client, certs *CertBundle) error {
	// Encode user certificate to PEM
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certs.UserCert.Raw,
	})

	// Create registration request
	reqBody := struct {
		Name    string `json:"name"`
		Role    string `json:"role"`
		CertPEM string `json:"cert_pem"`
	}{
		Name:    "testuser",
		Role:    "user",
		CertPEM: string(certPEM),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		adminClient.BaseURL+"/admin/v1/clients", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := adminClient.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("register client failed: %s", resp.Status)
	}

	return nil
}
