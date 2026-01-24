package gitlab

import (
	"context"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"gitlab.com/gitlab-org/api/client-go"
)

// Retry configuration constants.
const (
	maxRetries     = 3
	baseBackoffSec = 1 // 1 second base for exponential backoff
	maxBackoffSec  = 30
)

// retryableStatusCodes are HTTP status codes that warrant a retry.
var retryableStatusCodes = map[int]bool{
	http.StatusTooManyRequests:     true, // 429
	http.StatusServiceUnavailable:  true, // 503
	http.StatusInternalServerError: true, // 500 (may be transient)
	http.StatusBadGateway:          true, // 502
	http.StatusGatewayTimeout:      true, // 504
}

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: maxRetries,
		BaseDelay:  time.Duration(baseBackoffSec) * time.Second,
		MaxDelay:   time.Duration(maxBackoffSec) * time.Second,
	}
}

// shouldRetry determines if a response should be retried.
func shouldRetry(resp *gitlab.Response) bool {
	if resp == nil {
		return false
	}
	return retryableStatusCodes[resp.StatusCode]
}

// getRetryAfter extracts the Retry-After value from response headers.
// Returns 0 if header is not present or invalid.
func getRetryAfter(resp *gitlab.Response) time.Duration {
	if resp == nil || resp.Header == nil {
		return 0
	}

	header := resp.Header.Get("Retry-After")
	if header == "" {
		return 0
	}

	// Try parsing as seconds (most common)
	if seconds, err := strconv.Atoi(header); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP-date (less common)
	if t, err := http.ParseTime(header); err == nil {
		delay := time.Until(t)
		if delay > 0 {
			return delay
		}
	}

	return 0
}

// calculateBackoff calculates the backoff duration for a given attempt.
// Uses exponential backoff with jitter (±25%) capped at maxDelay.
// Jitter prevents thundering herd when multiple clients retry simultaneously.
func calculateBackoff(attempt int, cfg RetryConfig, retryAfter time.Duration) time.Duration {
	// If Retry-After header is present, use it (no jitter for server-specified delays)
	if retryAfter > 0 {
		if retryAfter > cfg.MaxDelay {
			return cfg.MaxDelay
		}
		return retryAfter
	}

	// Exponential backoff: base * 2^attempt
	backoff := float64(cfg.BaseDelay) * math.Pow(2, float64(attempt))

	// Cap at max delay before adding jitter
	if backoff > float64(cfg.MaxDelay) {
		backoff = float64(cfg.MaxDelay)
	}

	// Add jitter: ±25% of the backoff value
	// This spreads out retries to prevent thundering herd
	// Using math/rand is fine for jitter - no cryptographic requirement
	jitterRange := backoff * 0.25
	jitter := (rand.Float64() * 2 * jitterRange) - jitterRange //nolint:gosec // Non-crypto jitter is fine
	backoff += jitter

	// Ensure we don't go below minimum (1ms) or above max
	if backoff < float64(time.Millisecond) {
		backoff = float64(time.Millisecond)
	}
	if backoff > float64(cfg.MaxDelay) {
		backoff = float64(cfg.MaxDelay)
	}

	return time.Duration(backoff)
}

// sleep waits for the specified duration or until context is cancelled.
func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// RetryableFunc is a function that can be retried.
// It returns the response (for retry-after header inspection) and error.
type RetryableFunc func() (*gitlab.Response, error)

// WithRetry executes a function with retry logic.
// It respects Retry-After headers and uses exponential backoff.
func WithRetry(ctx context.Context, cfg RetryConfig, fn RetryableFunc) error {
	var lastErr error

	for attempt := range cfg.MaxRetries {
		resp, err := fn()

		// Success
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if retryable
		if !shouldRetry(resp) {
			return err
		}

		// Check context before sleeping
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Calculate backoff
		retryAfter := getRetryAfter(resp)
		backoff := calculateBackoff(attempt, cfg, retryAfter)

		// Sleep
		if sleepErr := sleep(ctx, backoff); sleepErr != nil {
			return sleepErr
		}
	}

	return lastErr
}

// WithRetryResult executes a function with retry logic and returns both result and error.
func WithRetryResult[T any](ctx context.Context, cfg RetryConfig, fn func() (T, *gitlab.Response, error)) (T, error) {
	var result T
	var lastErr error

	for attempt := range cfg.MaxRetries {
		var resp *gitlab.Response
		result, resp, lastErr = fn()

		// Success
		if lastErr == nil {
			return result, nil
		}

		// Check if retryable
		if !shouldRetry(resp) {
			return result, lastErr
		}

		// Check context before sleeping
		if ctx.Err() != nil {
			var zero T
			return zero, ctx.Err()
		}

		// Calculate backoff
		retryAfter := getRetryAfter(resp)
		backoff := calculateBackoff(attempt, cfg, retryAfter)

		// Sleep
		if sleepErr := sleep(ctx, backoff); sleepErr != nil {
			var zero T
			return zero, sleepErr
		}
	}

	return result, lastErr
}
