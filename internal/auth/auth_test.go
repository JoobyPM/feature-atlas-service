package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenData_IsExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{
			name:      "zero time (no expiry)",
			expiresAt: time.Time{},
			want:      false,
		},
		{
			name:      "expired",
			expiresAt: time.Now().Add(-1 * time.Hour),
			want:      true,
		},
		{
			name:      "not expired",
			expiresAt: time.Now().Add(1 * time.Hour),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := &TokenData{ExpiresAt: tt.expiresAt}
			assert.Equal(t, tt.want, token.IsExpired())
		})
	}
}

func TestTokenData_IsExpiringSoon(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		buffer    time.Duration
		want      bool
	}{
		{
			name:      "zero time (no expiry)",
			expiresAt: time.Time{},
			buffer:    5 * time.Minute,
			want:      false,
		},
		{
			name:      "expiring within buffer",
			expiresAt: time.Now().Add(3 * time.Minute),
			buffer:    5 * time.Minute,
			want:      true,
		},
		{
			name:      "not expiring within buffer",
			expiresAt: time.Now().Add(10 * time.Minute),
			buffer:    5 * time.Minute,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := &TokenData{ExpiresAt: tt.expiresAt}
			assert.Equal(t, tt.want, token.IsExpiringSoon(tt.buffer))
		})
	}
}

func TestTokenData_IsValid(t *testing.T) {
	assert.True(t, (&TokenData{AccessToken: "abc"}).IsValid())
	assert.False(t, (&TokenData{AccessToken: ""}).IsValid())
}

func TestCreateTokenFromPAT(t *testing.T) {
	token := CreateTokenFromPAT("glpat-xxx")
	assert.Equal(t, "glpat-xxx", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.True(t, token.ExpiresAt.IsZero(), "PAT should have no expiry")
}

func TestGitLabAuth_StartDeviceFlow(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/oauth/authorize_device", r.URL.Path)
			assert.Equal(t, "POST", r.Method)

			err := r.ParseForm()
			require.NoError(t, err)
			assert.Equal(t, "test-client-id", r.FormValue("client_id"))

			resp := DeviceAuthResponse{
				DeviceCode:      "device-code-123",
				UserCode:        "ABCD-1234",
				VerificationURI: "https://gitlab.com/oauth/device",
				ExpiresIn:       900,
				Interval:        5,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		auth := NewGitLabAuth(server.URL, "test-client-id")
		resp, err := auth.StartDeviceFlow(context.Background())

		require.NoError(t, err)
		assert.Equal(t, "device-code-123", resp.DeviceCode)
		assert.Equal(t, "ABCD-1234", resp.UserCode)
		assert.Equal(t, "https://gitlab.com/oauth/device", resp.VerificationURI)
	})

	t.Run("missing client ID", func(t *testing.T) {
		auth := NewGitLabAuth("https://gitlab.com", "")
		_, err := auth.StartDeviceFlow(context.Background())
		assert.ErrorIs(t, err, ErrMissingClientID)
	})

	t.Run("device flow not supported", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			resp := OAuth2Error{
				Error:            "invalid_request",
				ErrorDescription: "grant type not supported",
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		auth := NewGitLabAuth(server.URL, "test-client-id")
		_, err := auth.StartDeviceFlow(context.Background())
		assert.ErrorIs(t, err, ErrDeviceFlowNotSupported)
	})
}

func TestGitLabAuth_PollForToken(t *testing.T) {
	t.Run("success after pending", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/oauth/token", r.URL.Path)
			callCount++

			if callCount < 2 {
				// First call: pending
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(OAuth2Error{Error: "authorization_pending"})
				return
			}

			// Second call: success
			resp := TokenResponse{
				AccessToken:  "access-token-123",
				TokenType:    "Bearer",
				ExpiresIn:    7200,
				RefreshToken: "refresh-token-456",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		auth := NewGitLabAuth(server.URL, "test-client-id")
		auth.PollInterval = 10 * time.Millisecond // Speed up test

		token, err := auth.PollForToken(context.Background(), "device-code", 10*time.Millisecond)

		require.NoError(t, err)
		assert.Equal(t, "access-token-123", token.AccessToken)
		assert.Equal(t, "refresh-token-456", token.RefreshToken)
		assert.Equal(t, 2, callCount)
	})

	t.Run("access denied", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(OAuth2Error{Error: "access_denied"})
		}))
		defer server.Close()

		auth := NewGitLabAuth(server.URL, "test-client-id")
		auth.PollInterval = 10 * time.Millisecond

		_, err := auth.PollForToken(context.Background(), "device-code", 10*time.Millisecond)
		assert.ErrorIs(t, err, ErrAuthorizationDenied)
	})

	t.Run("context cancelled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(OAuth2Error{Error: "authorization_pending"})
		}))
		defer server.Close()

		auth := NewGitLabAuth(server.URL, "test-client-id")

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := auth.PollForToken(ctx, "device-code", 10*time.Millisecond)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestGitLabAuth_RefreshToken(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/oauth/token", r.URL.Path)

			err := r.ParseForm()
			require.NoError(t, err)
			assert.Equal(t, "refresh_token", r.FormValue("grant_type"))
			assert.Equal(t, "old-refresh-token", r.FormValue("refresh_token"))

			resp := TokenResponse{
				AccessToken:  "new-access-token",
				TokenType:    "Bearer",
				ExpiresIn:    7200,
				RefreshToken: "new-refresh-token",
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		auth := NewGitLabAuth(server.URL, "test-client-id")
		token, err := auth.RefreshToken(context.Background(), "old-refresh-token")

		require.NoError(t, err)
		assert.Equal(t, "new-access-token", token.AccessToken)
		assert.Equal(t, "new-refresh-token", token.RefreshToken)
	})

	t.Run("invalid grant", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(OAuth2Error{Error: "invalid_grant"})
		}))
		defer server.Close()

		auth := NewGitLabAuth(server.URL, "test-client-id")
		_, err := auth.RefreshToken(context.Background(), "invalid-token")
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})

	t.Run("empty refresh token", func(t *testing.T) {
		auth := NewGitLabAuth("https://gitlab.com", "test-client-id")
		_, err := auth.RefreshToken(context.Background(), "")
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})
}

func TestGitLabAuth_GetValidToken(t *testing.T) {
	t.Run("env token takes priority", func(t *testing.T) {
		auth := NewGitLabAuth("https://gitlab.com", "test-client-id")
		token, err := auth.GetValidToken(context.Background(), "env-token")

		require.NoError(t, err)
		assert.Equal(t, "env-token", token.AccessToken)
		assert.True(t, token.ExpiresAt.IsZero(), "env token should have no expiry")
	})
}

func TestIsHeadless(_ *testing.T) {
	// This test depends on environment, so just verify it doesn't panic
	_ = IsHeadless()
}

func TestKeyringKey(t *testing.T) {
	key := keyringKey("https://gitlab.com")
	assert.Equal(t, "gitlab:https://gitlab.com", key)

	key = keyringKey("https://gitlab.example.com")
	assert.Equal(t, "gitlab:https://gitlab.example.com", key)
}
