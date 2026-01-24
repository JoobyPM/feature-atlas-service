// Package auth provides authentication for featctl backends.
// It handles OAuth2 flows for GitLab and secure credential storage.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

// Keyring service name for featctl credentials.
const (
	ServiceName = "featctl"
)

// TokenData stores OAuth2 token information.
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope,omitempty"`
}

// Errors for keyring operations.
var (
	ErrNoCredential    = errors.New("no credential found")
	ErrKeyringNotAvail = errors.New("keyring not available")
)

// keyringKey generates a unique key for storing credentials.
// Format: "gitlab:<instance_url>" to support multiple instances.
func keyringKey(instanceURL string) string {
	return "gitlab:" + instanceURL
}

// StoreToken stores a token in the OS keyring.
// The token is serialized to JSON for storage.
func StoreToken(instanceURL string, token *TokenData) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	key := keyringKey(instanceURL)
	if err := keyring.Set(ServiceName, key, string(data)); err != nil {
		// Check if keyring is not available (e.g., headless environment)
		if isKeyringUnavailable(err) {
			return fmt.Errorf("%w: %w", ErrKeyringNotAvail, err)
		}
		return fmt.Errorf("store token: %w", err)
	}

	return nil
}

// LoadToken retrieves a token from the OS keyring.
// Returns ErrNoCredential if no token is stored.
func LoadToken(instanceURL string) (*TokenData, error) {
	key := keyringKey(instanceURL)
	data, err := keyring.Get(ServiceName, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, ErrNoCredential
		}
		if isKeyringUnavailable(err) {
			return nil, fmt.Errorf("%w: %w", ErrKeyringNotAvail, err)
		}
		return nil, fmt.Errorf("load token: %w", err)
	}

	var token TokenData
	if err := json.Unmarshal([]byte(data), &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}

	return &token, nil
}

// DeleteToken removes a token from the OS keyring.
// Returns nil if no token was stored (idempotent).
func DeleteToken(instanceURL string) error {
	key := keyringKey(instanceURL)
	err := keyring.Delete(ServiceName, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil // Already deleted
		}
		if isKeyringUnavailable(err) {
			return fmt.Errorf("%w: %w", ErrKeyringNotAvail, err)
		}
		return fmt.Errorf("delete token: %w", err)
	}
	return nil
}

// IsExpired checks if the token is expired or will expire within the buffer duration.
func (t *TokenData) IsExpired() bool {
	return t.IsExpiringSoon(0)
}

// IsExpiringSoon checks if the token will expire within the given buffer duration.
// Use this to refresh tokens before they actually expire.
func (t *TokenData) IsExpiringSoon(buffer time.Duration) bool {
	if t.ExpiresAt.IsZero() {
		return false // No expiry set (PAT tokens don't expire via OAuth)
	}
	return time.Now().Add(buffer).After(t.ExpiresAt)
}

// IsValid checks if the token has a non-empty access token.
func (t *TokenData) IsValid() bool {
	return t.AccessToken != ""
}

// isKeyringUnavailable checks if the error indicates the keyring is not available.
// This happens in headless environments (CI, containers, SSH sessions).
func isKeyringUnavailable(err error) bool {
	if err == nil {
		return false
	}
	// If it's ErrNotFound, the keyring IS available - just no data stored
	if errors.Is(err, keyring.ErrNotFound) {
		return false
	}
	// Check for common keyring unavailability errors
	errStr := err.Error()
	// Linux: dbus errors
	// macOS: keychain unavailable
	// Windows: credential manager errors
	return strings.Contains(errStr, "dbus") ||
		strings.Contains(errStr, "keychain") ||
		strings.Contains(errStr, "credential") ||
		strings.Contains(errStr, "secret service")
}

// IsKeyringAvailable checks if the OS keyring is available.
// Useful for deciding whether to prompt for interactive login.
func IsKeyringAvailable() bool {
	// Try a simple get operation - if keyring is unavailable, it will fail
	_, err := keyring.Get(ServiceName, "__test__")
	if err == nil {
		// Unexpectedly found something, clean it up (ignore error - best effort)
		//nolint:errcheck // Best effort cleanup, we already confirmed keyring works
		keyring.Delete(ServiceName, "__test__")
		return true
	}
	// ErrNotFound means keyring is available but key doesn't exist
	return errors.Is(err, keyring.ErrNotFound)
}

// IsHeadless detects if we're running in a headless environment.
// Returns true if running in CI, container, or SSH session without display.
func IsHeadless() bool {
	// Check for CI environment variables
	ciEnvVars := []string{
		"CI",
		"GITLAB_CI",
		"GITHUB_ACTIONS",
		"JENKINS_URL",
		"BUILDKITE",
		"CIRCLECI",
		"TRAVIS",
	}
	for _, env := range ciEnvVars {
		if os.Getenv(env) != "" {
			return true
		}
	}

	// Check for container environment
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	// Check for SSH session without display forwarding
	if os.Getenv("SSH_TTY") != "" && os.Getenv("DISPLAY") == "" {
		return true
	}

	return false
}
