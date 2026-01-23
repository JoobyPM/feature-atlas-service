// Package mock provides a mock implementation of FeatureBackend for testing.
package mock

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

// Backend is a mock implementation of backend.FeatureBackend.
// It stores features in memory and provides hooks for testing.
type Backend struct {
	mu       sync.RWMutex
	features map[string]backend.Feature
	authInfo *backend.AuthInfo
	mode     string

	// Test hooks - set these to inject errors or custom behavior
	SuggestFunc       func(ctx context.Context, query string, limit int) ([]backend.SuggestItem, error)
	SearchFunc        func(ctx context.Context, query string, limit int) ([]backend.Feature, error)
	GetFeatureFunc    func(ctx context.Context, id string) (*backend.Feature, error)
	FeatureExistsFunc func(ctx context.Context, id string) (bool, error)
	ListAllFunc       func(ctx context.Context) ([]backend.Feature, error)
	CreateFeatureFunc func(ctx context.Context, feature backend.Feature) (*backend.Feature, error)
	UpdateFeatureFunc func(ctx context.Context, id string, updates backend.Feature) (*backend.Feature, error)
	DeleteFeatureFunc func(ctx context.Context, id string) error
	GetAuthInfoFunc   func(ctx context.Context) (*backend.AuthInfo, error)

	// Tracking - for assertions in tests
	SuggestCalls       []SuggestCall
	SearchCalls        []SearchCall
	CreateFeatureCalls []CreateFeatureCall
	UpdateFeatureCalls []UpdateFeatureCall
	DeleteFeatureCalls []string
}

// SuggestCall records a Suggest call.
type SuggestCall struct {
	Query string
	Limit int
}

// SearchCall records a Search call.
type SearchCall struct {
	Query string
	Limit int
}

// CreateFeatureCall records a CreateFeature call.
type CreateFeatureCall struct {
	Feature backend.Feature
}

// UpdateFeatureCall records an UpdateFeature call.
type UpdateFeatureCall struct {
	ID      string
	Updates backend.Feature
}

// Ensure Backend implements FeatureBackend at compile time.
var _ backend.FeatureBackend = (*Backend)(nil)

// New creates a new mock backend with default atlas mode.
func New() *Backend {
	return &Backend{
		features: make(map[string]backend.Feature),
		authInfo: &backend.AuthInfo{
			Username:    "test-user",
			DisplayName: "Test User",
			Role:        "admin",
		},
		mode: backend.ModeAtlas,
	}
}

// NewWithMode creates a new mock backend with the specified mode.
func NewWithMode(mode string) *Backend {
	b := New()
	b.mode = mode
	return b
}

// Mode returns the backend mode identifier.
func (b *Backend) Mode() string {
	return b.mode
}

// AddFeature adds a feature to the mock store.
func (b *Backend) AddFeature(f backend.Feature) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.features[f.ID] = f
}

// AddFeatures adds multiple features to the mock store.
func (b *Backend) AddFeatures(features []backend.Feature) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, f := range features {
		b.features[f.ID] = f
	}
}

// SetAuthInfo sets the auth info returned by GetAuthInfo.
func (b *Backend) SetAuthInfo(info *backend.AuthInfo) {
	b.authInfo = info
}

// Suggest returns autocomplete suggestions for the given query.
func (b *Backend) Suggest(ctx context.Context, query string, limit int) ([]backend.SuggestItem, error) {
	b.mu.Lock()
	b.SuggestCalls = append(b.SuggestCalls, SuggestCall{Query: query, Limit: limit})
	b.mu.Unlock()

	if b.SuggestFunc != nil {
		return b.SuggestFunc(ctx, query, limit)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	var items []backend.SuggestItem
	for _, f := range b.features {
		if matchesQuery(f, query) {
			items = append(items, backend.SuggestItem{
				ID:      f.ID,
				Name:    f.Name,
				Summary: f.Summary,
			})
			if len(items) >= limit {
				break
			}
		}
	}
	return items, nil
}

// Search returns features matching the query.
func (b *Backend) Search(ctx context.Context, query string, limit int) ([]backend.Feature, error) {
	b.mu.Lock()
	b.SearchCalls = append(b.SearchCalls, SearchCall{Query: query, Limit: limit})
	b.mu.Unlock()

	if b.SearchFunc != nil {
		return b.SearchFunc(ctx, query, limit)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	var features []backend.Feature
	for _, f := range b.features {
		if matchesQuery(f, query) {
			features = append(features, f)
			if len(features) >= limit {
				break
			}
		}
	}
	return features, nil
}

// GetFeature retrieves a feature by ID.
func (b *Backend) GetFeature(ctx context.Context, id string) (*backend.Feature, error) {
	if b.GetFeatureFunc != nil {
		return b.GetFeatureFunc(ctx, id)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	f, ok := b.features[id]
	if !ok {
		return nil, backend.ErrNotFound
	}
	return &f, nil
}

// FeatureExists checks if a feature with the given ID exists.
func (b *Backend) FeatureExists(ctx context.Context, id string) (bool, error) {
	if b.FeatureExistsFunc != nil {
		return b.FeatureExistsFunc(ctx, id)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	_, ok := b.features[id]
	return ok, nil
}

// ListAll returns all features.
func (b *Backend) ListAll(ctx context.Context) ([]backend.Feature, error) {
	if b.ListAllFunc != nil {
		return b.ListAllFunc(ctx)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	features := make([]backend.Feature, 0, len(b.features))
	for _, f := range b.features {
		features = append(features, f)
	}
	return features, nil
}

// CreateFeature creates a new feature.
func (b *Backend) CreateFeature(ctx context.Context, feature backend.Feature) (*backend.Feature, error) {
	b.mu.Lock()
	b.CreateFeatureCalls = append(b.CreateFeatureCalls, CreateFeatureCall{Feature: feature})
	b.mu.Unlock()

	if b.CreateFeatureFunc != nil {
		return b.CreateFeatureFunc(ctx, feature)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Assign ID if not provided
	if feature.ID == "" {
		feature.ID = generateID(len(b.features) + 1)
	}

	// Check for duplicate
	if _, exists := b.features[feature.ID]; exists {
		return nil, backend.ErrAlreadyExists
	}

	// Set timestamps
	now := time.Now()
	feature.CreatedAt = now
	feature.UpdatedAt = now

	b.features[feature.ID] = feature
	return &feature, nil
}

// UpdateFeature updates an existing feature.
func (b *Backend) UpdateFeature(ctx context.Context, id string, updates backend.Feature) (*backend.Feature, error) {
	b.mu.Lock()
	b.UpdateFeatureCalls = append(b.UpdateFeatureCalls, UpdateFeatureCall{ID: id, Updates: updates})
	b.mu.Unlock()

	if b.UpdateFeatureFunc != nil {
		return b.UpdateFeatureFunc(ctx, id, updates)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	f, ok := b.features[id]
	if !ok {
		return nil, backend.ErrNotFound
	}

	// Apply updates
	if updates.Name != "" {
		f.Name = updates.Name
	}
	if updates.Summary != "" {
		f.Summary = updates.Summary
	}
	if updates.Owner != "" {
		f.Owner = updates.Owner
	}
	if updates.Tags != nil {
		f.Tags = updates.Tags
	}
	f.UpdatedAt = time.Now()

	b.features[id] = f
	return &f, nil
}

// DeleteFeature deletes a feature.
func (b *Backend) DeleteFeature(ctx context.Context, id string) error {
	b.mu.Lock()
	b.DeleteFeatureCalls = append(b.DeleteFeatureCalls, id)
	b.mu.Unlock()

	if b.DeleteFeatureFunc != nil {
		return b.DeleteFeatureFunc(ctx, id)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.features[id]; !ok {
		return backend.ErrNotFound
	}

	delete(b.features, id)
	return nil
}

// GetAuthInfo returns the authenticated client's information.
func (b *Backend) GetAuthInfo(ctx context.Context) (*backend.AuthInfo, error) {
	if b.GetAuthInfoFunc != nil {
		return b.GetAuthInfoFunc(ctx)
	}
	return b.authInfo, nil
}

// Reset clears all features and call tracking.
func (b *Backend) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.features = make(map[string]backend.Feature)
	b.SuggestCalls = nil
	b.SearchCalls = nil
	b.CreateFeatureCalls = nil
	b.UpdateFeatureCalls = nil
	b.DeleteFeatureCalls = nil
}

// matchesQuery checks if a feature matches the query (case-insensitive).
func matchesQuery(f backend.Feature, query string) bool {
	if query == "" {
		return true
	}
	query = strings.ToLower(query)
	return strings.Contains(strings.ToLower(f.ID), query) ||
		strings.Contains(strings.ToLower(f.Name), query) ||
		strings.Contains(strings.ToLower(f.Summary), query)
}

// generateID generates a feature ID in the format FT-NNNNNN.
func generateID(n int) string {
	return fmt.Sprintf("FT-%06d", n)
}
