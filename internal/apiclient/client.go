// Package apiclient provides an mTLS HTTP client for the feature-atlas API.
package apiclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Client is an mTLS-enabled HTTP client for the feature-atlas API.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// Feature represents a feature from the catalog.
type Feature struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Summary   string    `json:"summary"`
	Owner     string    `json:"owner"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
}

// SuggestItem represents a suggestion for autocomplete.
type SuggestItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// ClientInfo represents the authenticated client's information.
type ClientInfo struct {
	Name        string `json:"name"`
	Role        string `json:"role"`
	Fingerprint string `json:"fingerprint"`
	Subject     string `json:"subject"`
}

// ErrFeatureNotFound is returned when a feature doesn't exist.
var ErrFeatureNotFound = errors.New("feature not found")

// New creates a new mTLS-enabled API client.
func New(baseURL, caFile, certFile, keyFile string) (*Client, error) {
	//nolint:gosec // caFile is from trusted command-line flag
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("failed to parse CA PEM")
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	tlsCfg := &tls.Config{
		RootCAs:      pool,
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	tr := &http.Transport{TLSClientConfig: tlsCfg}

	return &Client{
		BaseURL: baseURL,
		HTTP: &http.Client{
			Transport: tr,
			Timeout:   10 * time.Second,
		},
	}, nil
}

// Me returns the authenticated client's information.
func (c *Client) Me(ctx context.Context) (*ClientInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/me", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("me failed: %s", resp.Status)
	}

	var info ClientInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	return &info, nil
}

// Suggest returns autocomplete suggestions for the given query.
func (c *Client) Suggest(ctx context.Context, query string, limit int) ([]SuggestItem, error) {
	u, err := url.Parse(c.BaseURL + "/api/v1/suggest")
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	q.Set("query", query)
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("suggest failed: %s", resp.Status)
	}

	var out struct {
		Items []SuggestItem `json:"items"`
		Count int           `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	return out.Items, nil
}

// Search returns features matching the query.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Feature, error) {
	u, err := url.Parse(c.BaseURL + "/api/v1/features")
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	q.Set("query", query)
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search failed: %s", resp.Status)
	}

	var out struct {
		Items []Feature `json:"items"`
		Count int       `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	return out.Items, nil
}

// GetFeature retrieves a feature by ID.
// Returns ErrFeatureNotFound if the feature doesn't exist.
func (c *Client) GetFeature(ctx context.Context, id string) (*Feature, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/features/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrFeatureNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get feature failed: %s", resp.Status)
	}

	var f Feature
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		return nil, err
	}

	return &f, nil
}

// FeatureExists checks if a feature with the given ID exists.
func (c *Client) FeatureExists(ctx context.Context, id string) (bool, error) {
	_, err := c.GetFeature(ctx, id)
	if errors.Is(err, ErrFeatureNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CreateFeatureRequest is the request body for creating a feature.
type CreateFeatureRequest struct {
	Name    string   `json:"name"`
	Summary string   `json:"summary"`
	Owner   string   `json:"owner,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// CreateFeature creates a new feature on the server (admin only).
// Returns the created feature with the server-assigned ID.
func (c *Client) CreateFeature(ctx context.Context, req CreateFeatureRequest) (*Feature, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/admin/v1/features", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.ContentLength = int64(len(body))

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusCreated:
		var f Feature
		if decodeErr := json.NewDecoder(resp.Body).Decode(&f); decodeErr != nil {
			return nil, decodeErr
		}
		return &f, nil
	case http.StatusForbidden:
		return nil, errors.New("admin role required")
	case http.StatusBadRequest:
		return nil, errors.New("invalid request: name and summary required")
	default:
		return nil, fmt.Errorf("create feature failed: %s", resp.Status)
	}
}
