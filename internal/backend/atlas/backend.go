// Package atlas provides the Atlas backend implementation.
// It wraps the existing apiclient.Client to implement the FeatureBackend interface.
package atlas

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
	"github.com/JoobyPM/feature-atlas-service/internal/backend"
	"github.com/JoobyPM/feature-atlas-service/internal/config"
)

// Backend implements backend.FeatureBackend for the Atlas server.
type Backend struct {
	client *apiclient.Client
}

// Ensure Backend implements FeatureBackend at compile time.
var _ backend.FeatureBackend = (*Backend)(nil)

// New creates a new Atlas backend from configuration.
func New(cfg config.AtlasConfig) (*Backend, error) {
	client, err := apiclient.New(cfg.ServerURL, cfg.CACert, cfg.Cert, cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("create atlas client: %w", err)
	}
	return &Backend{client: client}, nil
}

// NewFromClient creates a new Atlas backend from an existing client.
// Useful for testing or when the client is already initialized.
func NewFromClient(client *apiclient.Client) *Backend {
	return &Backend{client: client}
}

// Mode returns the backend mode identifier.
func (b *Backend) Mode() string {
	return backend.ModeAtlas
}

// InstanceID returns a unique identifier for this backend instance.
// Format: "atlas:<server-url>"
func (b *Backend) InstanceID() string {
	return "atlas:" + b.client.BaseURL
}

// Suggest returns autocomplete suggestions for the given query.
func (b *Backend) Suggest(ctx context.Context, query string, limit int) ([]backend.SuggestItem, error) {
	items, err := b.client.Suggest(ctx, query, limit)
	if err != nil {
		return nil, mapError(err)
	}

	result := make([]backend.SuggestItem, len(items))
	for i, item := range items {
		result[i] = backend.SuggestItem{
			ID:      item.ID,
			Name:    item.Name,
			Summary: item.Summary,
		}
	}
	return result, nil
}

// Search returns features matching the query.
func (b *Backend) Search(ctx context.Context, query string, limit int) ([]backend.Feature, error) {
	features, err := b.client.Search(ctx, query, limit)
	if err != nil {
		return nil, mapError(err)
	}

	result := make([]backend.Feature, len(features))
	for i, f := range features {
		result[i] = featureFromAPI(f)
	}
	return result, nil
}

// GetFeature retrieves a feature by ID.
func (b *Backend) GetFeature(ctx context.Context, id string) (*backend.Feature, error) {
	f, err := b.client.GetFeature(ctx, id)
	if err != nil {
		return nil, mapError(err)
	}

	result := featureFromAPI(*f)
	return &result, nil
}

// FeatureExists checks if a feature with the given ID exists.
func (b *Backend) FeatureExists(ctx context.Context, id string) (bool, error) {
	exists, err := b.client.FeatureExists(ctx, id)
	if err != nil {
		return false, mapError(err)
	}
	return exists, nil
}

// ListAll returns all features from the server.
// Uses Search with empty query and high limit.
func (b *Backend) ListAll(ctx context.Context) ([]backend.Feature, error) {
	// Atlas API uses Search for listing - request a large batch
	const maxLimit = 1000
	return b.Search(ctx, "", maxLimit)
}

// CreateFeature creates a new feature on the server.
// The server assigns the ID; input Feature.ID is ignored.
func (b *Backend) CreateFeature(ctx context.Context, feature backend.Feature) (*backend.Feature, error) {
	req := apiclient.CreateFeatureRequest{
		Name:    feature.Name,
		Summary: feature.Summary,
		Owner:   feature.Owner,
		Tags:    feature.Tags,
	}

	f, err := b.client.CreateFeature(ctx, req)
	if err != nil {
		return nil, mapError(err)
	}

	result := featureFromAPI(*f)
	return &result, nil
}

// UpdateFeature is not supported by Atlas backend.
// The Atlas server API doesn't support feature updates.
func (b *Backend) UpdateFeature(_ context.Context, _ string, _ backend.Feature) (*backend.Feature, error) {
	return nil, backend.ErrNotSupported
}

// DeleteFeature is not supported by Atlas backend.
// The Atlas server API doesn't support feature deletion.
func (b *Backend) DeleteFeature(_ context.Context, _ string) error {
	return backend.ErrNotSupported
}

// GetAuthInfo returns the authenticated client's information.
func (b *Backend) GetAuthInfo(ctx context.Context) (*backend.AuthInfo, error) {
	info, err := b.client.Me(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	return &backend.AuthInfo{
		Username:    info.Name,
		DisplayName: info.Name,
		Role:        info.Role,
	}, nil
}

// featureFromAPI converts an apiclient.Feature to backend.Feature.
func featureFromAPI(f apiclient.Feature) backend.Feature {
	return backend.Feature{
		ID:        f.ID,
		Name:      f.Name,
		Summary:   f.Summary,
		Owner:     f.Owner,
		Tags:      f.Tags,
		CreatedAt: f.CreatedAt,
		// Atlas API doesn't provide UpdatedAt - use CreatedAt as fallback
		UpdatedAt: f.CreatedAt,
	}
}

// mapError converts apiclient and network errors to backend errors.
func mapError(err error) error {
	if err == nil {
		return nil
	}

	// Check for specific apiclient errors
	if errors.Is(err, apiclient.ErrFeatureNotFound) {
		return backend.ErrNotFound
	}

	// Check for network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return fmt.Errorf("%w: %w", backend.ErrBackendOffline, err)
		}
		return fmt.Errorf("%w: %w", backend.ErrBackendOffline, err)
	}

	// Check for connection refused
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return fmt.Errorf("%w: %w", backend.ErrBackendOffline, err)
	}

	// Check error message for common patterns
	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "admin"):
		return fmt.Errorf("%w: %s", backend.ErrPermission, errMsg)
	case strings.Contains(errMsg, "name and summary required"):
		return fmt.Errorf("%w: %s", backend.ErrInvalidRequest, errMsg)
	case strings.Contains(errMsg, "connection refused"):
		return fmt.Errorf("%w: %w", backend.ErrBackendOffline, err)
	case strings.Contains(errMsg, "no such host"):
		return fmt.Errorf("%w: %w", backend.ErrBackendOffline, err)
	}

	// Return original error wrapped
	return err
}
