package gitlab

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/gitlab-org/api/client-go"
)

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       bool
	}{
		{"429 Too Many Requests", http.StatusTooManyRequests, true},
		{"503 Service Unavailable", http.StatusServiceUnavailable, true},
		{"500 Internal Server Error", http.StatusInternalServerError, true},
		{"502 Bad Gateway", http.StatusBadGateway, true},
		{"504 Gateway Timeout", http.StatusGatewayTimeout, true},
		{"200 OK", http.StatusOK, false},
		{"404 Not Found", http.StatusNotFound, false},
		{"401 Unauthorized", http.StatusUnauthorized, false},
		{"400 Bad Request", http.StatusBadRequest, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &gitlab.Response{
				Response: &http.Response{
					StatusCode: tt.statusCode,
				},
			}
			got := shouldRetry(resp)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("nil response", func(t *testing.T) {
		assert.False(t, shouldRetry(nil))
	})
}

func TestGetRetryAfter(t *testing.T) {
	t.Run("seconds format", func(t *testing.T) {
		resp := &gitlab.Response{
			Response: &http.Response{
				Header: http.Header{
					"Retry-After": []string{"60"},
				},
			},
		}
		got := getRetryAfter(resp)
		assert.Equal(t, 60*time.Second, got)
	})

	t.Run("no header", func(t *testing.T) {
		resp := &gitlab.Response{
			Response: &http.Response{
				Header: http.Header{},
			},
		}
		got := getRetryAfter(resp)
		assert.Equal(t, time.Duration(0), got)
	})

	t.Run("nil response", func(t *testing.T) {
		got := getRetryAfter(nil)
		assert.Equal(t, time.Duration(0), got)
	})

	t.Run("invalid value", func(t *testing.T) {
		resp := &gitlab.Response{
			Response: &http.Response{
				Header: http.Header{
					"Retry-After": []string{"not-a-number"},
				},
			},
		}
		got := getRetryAfter(resp)
		assert.Equal(t, time.Duration(0), got)
	})
}

func TestCalculateBackoff(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries: 3,
		BaseDelay:  time.Second,
		MaxDelay:   30 * time.Second,
	}

	t.Run("exponential backoff with jitter", func(t *testing.T) {
		// Jitter is ±25%, so we test within bounds
		// attempt 0: 1s * 2^0 = 1s ± 25% -> [0.75s, 1.25s]
		got := calculateBackoff(0, cfg, 0)
		assert.GreaterOrEqual(t, got, 750*time.Millisecond)
		assert.LessOrEqual(t, got, 1250*time.Millisecond)

		// attempt 1: 1s * 2^1 = 2s ± 25% -> [1.5s, 2.5s]
		got = calculateBackoff(1, cfg, 0)
		assert.GreaterOrEqual(t, got, 1500*time.Millisecond)
		assert.LessOrEqual(t, got, 2500*time.Millisecond)

		// attempt 2: 1s * 2^2 = 4s ± 25% -> [3s, 5s]
		got = calculateBackoff(2, cfg, 0)
		assert.GreaterOrEqual(t, got, 3*time.Second)
		assert.LessOrEqual(t, got, 5*time.Second)
	})

	t.Run("respects Retry-After without jitter", func(t *testing.T) {
		// Retry-After should be used exactly (no jitter for server-specified delays)
		retryAfter := 10 * time.Second
		got := calculateBackoff(0, cfg, retryAfter)
		assert.Equal(t, retryAfter, got)
	})

	t.Run("caps at max delay", func(t *testing.T) {
		// With very high attempt, should cap at max (with possible jitter down)
		got := calculateBackoff(10, cfg, 0)
		assert.LessOrEqual(t, got, cfg.MaxDelay)
		// Should still be close to max (within jitter range)
		assert.GreaterOrEqual(t, got, cfg.MaxDelay*75/100)

		// Retry-After exceeding max should be capped exactly
		got = calculateBackoff(0, cfg, 60*time.Second)
		assert.Equal(t, cfg.MaxDelay, got)
	})
}

func TestWithRetrySuccess(t *testing.T) {
	cfg := DefaultRetryConfig()
	ctx := context.Background()

	callCount := 0
	fn := func() (*gitlab.Response, error) {
		callCount++
		return &gitlab.Response{
			Response: &http.Response{StatusCode: http.StatusOK},
		}, nil
	}

	err := WithRetry(ctx, cfg, fn)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestWithRetryRetryOnError(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond, // Fast for testing
		MaxDelay:   100 * time.Millisecond,
	}
	ctx := context.Background()

	callCount := 0
	fn := func() (*gitlab.Response, error) {
		callCount++
		if callCount < 3 {
			return &gitlab.Response{
				Response: &http.Response{StatusCode: http.StatusTooManyRequests},
			}, errors.New("rate limited")
		}
		return &gitlab.Response{
			Response: &http.Response{StatusCode: http.StatusOK},
		}, nil
	}

	err := WithRetry(ctx, cfg, fn)
	require.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestWithRetryMaxRetriesExceeded(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   100 * time.Millisecond,
	}
	ctx := context.Background()

	callCount := 0
	testErr := errors.New("always fail")
	fn := func() (*gitlab.Response, error) {
		callCount++
		return &gitlab.Response{
			Response: &http.Response{StatusCode: http.StatusTooManyRequests},
		}, testErr
	}

	err := WithRetry(ctx, cfg, fn)
	require.Error(t, err)
	assert.Equal(t, cfg.MaxRetries, callCount)
}

func TestWithRetryNonRetryableError(t *testing.T) {
	cfg := DefaultRetryConfig()
	ctx := context.Background()

	callCount := 0
	fn := func() (*gitlab.Response, error) {
		callCount++
		return &gitlab.Response{
			Response: &http.Response{StatusCode: http.StatusNotFound},
		}, errors.New("not found")
	}

	err := WithRetry(ctx, cfg, fn)
	require.Error(t, err)
	assert.Equal(t, 1, callCount) // Should not retry
}

func TestWithRetryContextCancelled(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries: 3,
		BaseDelay:  time.Second, // Long delay
		MaxDelay:   10 * time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	fn := func() (*gitlab.Response, error) {
		callCount++
		if callCount == 1 {
			// Cancel context after first call
			cancel()
		}
		return &gitlab.Response{
			Response: &http.Response{StatusCode: http.StatusTooManyRequests},
		}, errors.New("rate limited")
	}

	err := WithRetry(ctx, cfg, fn)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWithRetryResult(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries: 3,
		BaseDelay:  10 * time.Millisecond,
		MaxDelay:   100 * time.Millisecond,
	}
	ctx := context.Background()

	callCount := 0
	fn := func() (string, *gitlab.Response, error) {
		callCount++
		if callCount < 2 {
			return "", &gitlab.Response{
				Response: &http.Response{StatusCode: http.StatusServiceUnavailable},
			}, errors.New("service unavailable")
		}
		return "success", &gitlab.Response{
			Response: &http.Response{StatusCode: http.StatusOK},
		}, nil
	}

	result, err := WithRetryResult(ctx, cfg, fn)
	require.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 2, callCount)
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	assert.Equal(t, maxRetries, cfg.MaxRetries)
	assert.Equal(t, time.Duration(baseBackoffSec)*time.Second, cfg.BaseDelay)
	assert.Equal(t, time.Duration(maxBackoffSec)*time.Second, cfg.MaxDelay)
}
