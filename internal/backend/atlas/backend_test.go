package atlas

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

func TestBackendMode(t *testing.T) {
	b := &Backend{}
	assert.Equal(t, backend.ModeAtlas, b.Mode())
}

func TestBackendUpdateFeatureNotSupported(t *testing.T) {
	b := &Backend{}
	_, err := b.UpdateFeature(context.Background(), "FT-000001", backend.Feature{})
	assert.ErrorIs(t, err, backend.ErrNotSupported)
}

func TestBackendDeleteFeatureNotSupported(t *testing.T) {
	b := &Backend{}
	err := b.DeleteFeature(context.Background(), "FT-000001")
	assert.ErrorIs(t, err, backend.ErrNotSupported)
}

func TestMapError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantErr  error
		contains string
	}{
		{
			name:    "nil error",
			err:     nil,
			wantErr: nil,
		},
		{
			name:    "feature not found",
			err:     apiclient.ErrFeatureNotFound,
			wantErr: backend.ErrNotFound,
		},
		{
			name:     "admin required",
			err:      errors.New("admin role required"),
			wantErr:  backend.ErrPermission,
			contains: "admin",
		},
		{
			name:     "invalid request",
			err:      errors.New("invalid request: name and summary required"),
			wantErr:  backend.ErrInvalidRequest,
			contains: "name and summary required",
		},
		{
			name:    "network timeout",
			err:     &net.OpError{Op: "dial", Err: &timeoutError{}},
			wantErr: backend.ErrBackendOffline,
		},
		{
			name:     "connection refused",
			err:      errors.New("connection refused"),
			wantErr:  backend.ErrBackendOffline,
			contains: "connection refused",
		},
		{
			name:     "no such host",
			err:      errors.New("no such host"),
			wantErr:  backend.ErrBackendOffline,
			contains: "no such host",
		},
		{
			name:     "other error",
			err:      errors.New("some other error"),
			wantErr:  nil, // Should return original error
			contains: "some other error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapError(tt.err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, result, tt.wantErr)
			}
			if tt.contains != "" && result != nil {
				assert.Contains(t, result.Error(), tt.contains)
			}
			if tt.err == nil {
				assert.Nil(t, result)
			}
		})
	}
}

// timeoutError is a mock net.Error for testing.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

// Verify interface compliance at compile time.
func TestInterfaceCompliance(_ *testing.T) {
	var _ backend.FeatureBackend = (*Backend)(nil)
}
