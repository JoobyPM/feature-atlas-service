package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GitLab OAuth2 endpoints (relative to instance URL).
const (
	deviceAuthEndpoint = "/oauth/authorize_device"
	tokenEndpoint      = "/oauth/token" //nolint:gosec // OAuth endpoint path, not a credential
)

// OAuth2 grant types.
const (
	grantTypeDeviceCode   = "urn:ietf:params:oauth:grant-type:device_code"
	grantTypeRefreshToken = "refresh_token"
)

// Default polling configuration.
const (
	DefaultPollInterval = 5 * time.Second
	DefaultPollTimeout  = 5 * time.Minute
	TokenRefreshBuffer  = 5 * time.Minute // Refresh when less than 5 min remaining
)

// DeviceAuthResponse is the response from the device authorization endpoint.
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"` // Polling interval in seconds
}

// TokenResponse is the response from the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	CreatedAt    int64  `json:"created_at,omitempty"`
}

// OAuth2Error represents an OAuth2 error response.
type OAuth2Error struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// GitLabAuth handles GitLab OAuth2 authentication.
type GitLabAuth struct {
	InstanceURL   string
	ClientID      string
	HTTPClient    *http.Client
	PollInterval  time.Duration
	PollTimeout   time.Duration
	RefreshBuffer time.Duration
}

// Errors for GitLab authentication.
var (
	ErrDeviceFlowNotSupported = errors.New("device authorization grant not supported (GitLab 17.2+ required)")
	ErrAuthorizationPending   = errors.New("authorization pending")
	ErrSlowDown               = errors.New("slow down")
	ErrAuthorizationDenied    = errors.New("authorization denied by user")
	ErrExpiredToken           = errors.New("device code expired")
	ErrInvalidGrant           = errors.New("invalid or expired grant")
	ErrMissingClientID        = errors.New("oauth_client_id not configured")
)

// NewGitLabAuth creates a new GitLab authenticator.
func NewGitLabAuth(instanceURL, clientID string) *GitLabAuth {
	return &GitLabAuth{
		InstanceURL:   strings.TrimSuffix(instanceURL, "/"),
		ClientID:      clientID,
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
		PollInterval:  DefaultPollInterval,
		PollTimeout:   DefaultPollTimeout,
		RefreshBuffer: TokenRefreshBuffer,
	}
}

// StartDeviceFlow initiates the OAuth2 Device Authorization Grant flow.
// Returns the device auth response containing the user code and verification URL.
func (g *GitLabAuth) StartDeviceFlow(ctx context.Context) (*DeviceAuthResponse, error) {
	if g.ClientID == "" {
		return nil, ErrMissingClientID
	}

	endpoint := g.InstanceURL + deviceAuthEndpoint

	data := url.Values{}
	data.Set("client_id", g.ClientID)
	data.Set("scope", "api") // Full API access for read/write

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device auth request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Check for specific error
		var oauthErr OAuth2Error
		if json.Unmarshal(body, &oauthErr) == nil && oauthErr.Error != "" {
			if oauthErr.Error == "invalid_request" && strings.Contains(oauthErr.ErrorDescription, "not supported") {
				return nil, ErrDeviceFlowNotSupported
			}
			return nil, fmt.Errorf("oauth error: %s - %s", oauthErr.Error, oauthErr.ErrorDescription)
		}
		return nil, fmt.Errorf("device auth failed: %s", resp.Status)
	}

	var authResp DeviceAuthResponse
	if err := json.Unmarshal(body, &authResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &authResp, nil
}

// PollForToken polls the token endpoint until the user authorizes or an error occurs.
// This should be called after StartDeviceFlow and displaying the user code to the user.
func (g *GitLabAuth) PollForToken(ctx context.Context, deviceCode string, interval time.Duration) (*TokenData, error) {
	if interval < time.Second {
		interval = g.PollInterval
	}

	endpoint := g.InstanceURL + tokenEndpoint
	deadline := time.Now().Add(g.PollTimeout)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
			// Continue to poll
		}

		if time.Now().After(deadline) {
			return nil, ErrExpiredToken
		}

		token, err := g.requestToken(ctx, endpoint, deviceCode)
		if err == nil {
			return token, nil
		}

		// Handle polling errors
		if errors.Is(err, ErrAuthorizationPending) {
			continue // Keep polling
		}
		if errors.Is(err, ErrSlowDown) {
			interval += 5 * time.Second // Increase interval
			continue
		}
		// Other errors are terminal
		return nil, err
	}
}

// requestToken makes a single token request.
func (g *GitLabAuth) requestToken(ctx context.Context, endpoint, deviceCode string) (*TokenData, error) {
	data := url.Values{}
	data.Set("client_id", g.ClientID)
	data.Set("device_code", deviceCode)
	data.Set("grant_type", grantTypeDeviceCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Check for error response
	if resp.StatusCode != http.StatusOK {
		var oauthErr OAuth2Error
		if json.Unmarshal(body, &oauthErr) == nil {
			switch oauthErr.Error {
			case "authorization_pending":
				return nil, ErrAuthorizationPending
			case "slow_down":
				return nil, ErrSlowDown
			case "access_denied":
				return nil, ErrAuthorizationDenied
			case "expired_token":
				return nil, ErrExpiredToken
			default:
				return nil, fmt.Errorf("oauth error: %s - %s", oauthErr.Error, oauthErr.ErrorDescription)
			}
		}
		return nil, fmt.Errorf("token request failed: %s", resp.Status)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	// Calculate expiry time
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	if tokenResp.CreatedAt > 0 {
		expiresAt = time.Unix(tokenResp.CreatedAt, 0).Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return &TokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    expiresAt,
		Scope:        tokenResp.Scope,
	}, nil
}

// RefreshToken refreshes an expired or expiring token.
// Returns the new token or an error if refresh fails.
func (g *GitLabAuth) RefreshToken(ctx context.Context, refreshToken string) (*TokenData, error) {
	if g.ClientID == "" {
		return nil, ErrMissingClientID
	}
	if refreshToken == "" {
		return nil, ErrInvalidGrant
	}

	endpoint := g.InstanceURL + tokenEndpoint

	data := url.Values{}
	data.Set("client_id", g.ClientID)
	data.Set("refresh_token", refreshToken)
	data.Set("grant_type", grantTypeRefreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var oauthErr OAuth2Error
		if json.Unmarshal(body, &oauthErr) == nil && oauthErr.Error != "" {
			if oauthErr.Error == "invalid_grant" {
				return nil, ErrInvalidGrant
			}
			return nil, fmt.Errorf("oauth error: %s - %s", oauthErr.Error, oauthErr.ErrorDescription)
		}
		return nil, fmt.Errorf("refresh failed: %s", resp.Status)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	if tokenResp.CreatedAt > 0 {
		expiresAt = time.Unix(tokenResp.CreatedAt, 0).Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return &TokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    expiresAt,
		Scope:        tokenResp.Scope,
	}, nil
}

// GetValidToken retrieves a valid token, refreshing if necessary.
// Priority: env var > keyring
// If the token is expiring soon, it will be refreshed automatically.
func (g *GitLabAuth) GetValidToken(ctx context.Context, envToken string) (*TokenData, error) {
	// 1. Check environment variable (highest priority)
	if envToken != "" {
		return &TokenData{
			AccessToken: envToken,
			TokenType:   "Bearer",
			// No expiry for env-provided tokens (assumed to be PAT or CI token)
		}, nil
	}

	// 2. Load from keyring
	token, err := LoadToken(g.InstanceURL)
	if err != nil {
		return nil, err
	}

	// 3. Check if token needs refresh
	if token.IsExpiringSoon(g.RefreshBuffer) && token.RefreshToken != "" {
		newToken, refreshErr := g.RefreshToken(ctx, token.RefreshToken)
		if refreshErr != nil {
			// If refresh fails but token is still valid, use it
			if !token.IsExpired() {
				return token, nil
			}
			return nil, fmt.Errorf("token expired and refresh failed: %w", refreshErr)
		}

		// Store refreshed token (best effort - ignore errors since we have a valid token)
		_ = StoreToken(g.InstanceURL, newToken) //nolint:errcheck // Best effort, we have valid token
		return newToken, nil
	}

	// 4. Check if token is expired
	if token.IsExpired() {
		return nil, errors.New("token expired (no refresh token available)")
	}

	return token, nil
}

// CreateTokenFromPAT creates a TokenData from a Personal Access Token.
// PATs don't expire via OAuth, so ExpiresAt is set to zero.
func CreateTokenFromPAT(pat string) *TokenData {
	return &TokenData{
		AccessToken: pat,
		TokenType:   "Bearer",
		// ExpiresAt zero means no expiry
	}
}
