// Package backend defines the FeatureBackend interface for dual-mode operation.
// Both Atlas and GitLab backends implement this interface, enabling the CLI/TUI
// to work with either backend through a unified API.
package backend

import (
	"context"
	"errors"
	"time"
)

// Mode constants for backend identification.
const (
	ModeAtlas  = "atlas"
	ModeGitLab = "gitlab"
)

// Feature is the backend-agnostic feature type.
// This is distinct from apiclient.Feature and manifest.Feature.
// Both backend implementations translate to/from this type.
type Feature struct {
	ID        string // FT-NNNNNN (server) or FT-LOCAL-* (unsynced)
	Name      string
	Summary   string
	Owner     string
	Tags      []string
	CreatedAt time.Time // When feature was created
	UpdatedAt time.Time // When feature was last modified (for conflict detection)
}

// SuggestItem is the backend-agnostic autocomplete item.
// Used for search suggestions in the TUI.
type SuggestItem struct {
	ID      string
	Name    string
	Summary string
}

// AuthInfo provides authenticated user information.
type AuthInfo struct {
	Username    string // Certificate CN (Atlas) or GitLab username
	DisplayName string // Human-readable name
	Role        string // "admin"/"user" (Atlas) or "owner"/"maintainer"/"developer" (GitLab)
}

// FeatureBackend is the interface both Atlas and GitLab backends implement.
// All CLI and TUI operations go through this interface (DRY/SST compliance).
type FeatureBackend interface {
	// Read operations
	Suggest(ctx context.Context, query string, limit int) ([]SuggestItem, error)
	Search(ctx context.Context, query string, limit int) ([]Feature, error)
	GetFeature(ctx context.Context, id string) (*Feature, error)
	FeatureExists(ctx context.Context, id string) (bool, error)
	ListAll(ctx context.Context) ([]Feature, error) // For cache population

	// Write operations
	// CreateFeature: Input Feature.ID may be empty (backend assigns) or FT-LOCAL-* (GitLab tracks)
	// Output Feature always has the canonical ID assigned by backend
	CreateFeature(ctx context.Context, feature Feature) (*Feature, error)
	UpdateFeature(ctx context.Context, id string, updates Feature) (*Feature, error)
	DeleteFeature(ctx context.Context, id string) error

	// Info
	Mode() string // Returns "atlas" or "gitlab"
	GetAuthInfo(ctx context.Context) (*AuthInfo, error)
}

// Backend-agnostic errors.
// These errors should be used by all backend implementations for consistent
// error handling in the CLI/TUI layer.
var (
	// ErrNotFound is returned when a feature doesn't exist.
	ErrNotFound = errors.New("feature not found")

	// ErrAlreadyExists is returned when creating a feature that already exists.
	ErrAlreadyExists = errors.New("feature already exists")

	// ErrInvalidID is returned when a feature ID has invalid format.
	ErrInvalidID = errors.New("invalid feature ID format")

	// ErrPermission is returned when the user lacks permission for an operation.
	ErrPermission = errors.New("permission denied")

	// ErrBackendOffline is returned when the backend is unreachable.
	ErrBackendOffline = errors.New("backend not reachable")

	// ErrConflict is returned on concurrent update conflicts.
	ErrConflict = errors.New("conflict in concurrent update")

	// ErrNotSupported is returned when an operation is not supported by the backend.
	// For example, Atlas doesn't support UpdateFeature or DeleteFeature.
	ErrNotSupported = errors.New("operation not supported")

	// ErrRateLimited is returned when the backend rate limits the request.
	ErrRateLimited = errors.New("rate limited")

	// ErrInvalidRequest is returned when the request is malformed.
	ErrInvalidRequest = errors.New("invalid request")
)

// IsRetryable returns true if the error is potentially transient and
// the operation should be retried.
func IsRetryable(err error) bool {
	return errors.Is(err, ErrBackendOffline) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrConflict)
}
