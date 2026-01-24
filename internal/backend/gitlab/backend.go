package gitlab

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"gitlab.com/gitlab-org/api/client-go"
	"golang.org/x/oauth2"

	"github.com/JoobyPM/feature-atlas-service/internal/auth"
	"github.com/JoobyPM/feature-atlas-service/internal/backend"
	"github.com/JoobyPM/feature-atlas-service/internal/config"
)

// Backend implements backend.FeatureBackend for GitLab repositories.
type Backend struct {
	client        *gitlab.Client
	project       string // Project path or ID
	mainBranch    string // Main branch name (source of truth)
	instanceURL   string
	oauthClientID string
	mrConfig      MRConfig // MR creation settings

	// Cached features (loaded lazily)
	mu             sync.RWMutex
	cachedFeatures []backend.Feature
	cacheLoaded    bool
}

// Ensure Backend implements FeatureBackend at compile time.
var _ backend.FeatureBackend = (*Backend)(nil)

// New creates a new GitLab backend from configuration.
func New(cfg config.GitLabConfig) (*Backend, error) {
	if cfg.Project == "" {
		return nil, backend.ErrInvalidRequest
	}

	// Resolve token and determine its type
	// Priority: env var (PAT) > keyring (OAuth)
	token := cfg.Token
	isOAuthToken := false

	if token == "" {
		// Try to get from keyring (OAuth token)
		tokenData, err := auth.LoadToken(cfg.Instance)
		if err == nil && tokenData.IsValid() {
			token = tokenData.AccessToken
			isOAuthToken = true
		}
	}

	// Create GitLab client options
	opts := []gitlab.ClientOptionFunc{
		gitlab.WithBaseURL(cfg.Instance),
	}

	// Create client with appropriate auth type
	// - PATs use PRIVATE-TOKEN header (NewClient)
	// - OAuth tokens use Authorization: Bearer header (NewAuthSourceClient)
	var client *gitlab.Client
	var err error

	if token != "" {
		if isOAuthToken {
			// OAuth tokens require Bearer authentication
			ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
			client, err = gitlab.NewAuthSourceClient(gitlab.OAuthTokenSource{TokenSource: ts}, opts...)
		} else {
			// PATs and CI tokens use PRIVATE-TOKEN header
			client, err = gitlab.NewClient(token, opts...)
		}
	} else {
		// Try without auth (for public repos)
		client, err = gitlab.NewClient("", opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("create gitlab client: %w", err)
	}

	mainBranch := cfg.MainBranch
	if mainBranch == "" {
		mainBranch = "main"
	}

	mrCfg := MRConfig{
		Labels:             cfg.MRLabels,
		RemoveSourceBranch: cfg.MRRemoveSourceBranch,
		DefaultAssignee:    cfg.DefaultAssignee,
	}

	return &Backend{
		client:        client,
		project:       cfg.Project,
		mainBranch:    mainBranch,
		instanceURL:   cfg.Instance,
		oauthClientID: cfg.OAuthClientID,
		mrConfig:      mrCfg,
	}, nil
}

// NewFromClient creates a GitLab backend from an existing client.
// Useful for testing with mock clients.
func NewFromClient(client *gitlab.Client, project, mainBranch string) *Backend {
	if mainBranch == "" {
		mainBranch = "main"
	}
	return &Backend{
		client:     client,
		project:    project,
		mainBranch: mainBranch,
	}
}

// Mode returns the backend mode identifier.
func (b *Backend) Mode() string {
	return backend.ModeGitLab
}

// InstanceID returns a unique identifier for this backend instance.
// Format: "gitlab:<instance>/<project>"
func (b *Backend) InstanceID() string {
	return fmt.Sprintf("gitlab:%s/%s", b.instanceURL, b.project)
}

// Suggest returns autocomplete suggestions for the given query.
func (b *Backend) Suggest(ctx context.Context, query string, limit int) ([]backend.SuggestItem, error) {
	features, err := b.loadFeatures(ctx)
	if err != nil {
		return nil, err
	}

	return SuggestFromFeatures(features, query, limit), nil
}

// Search returns features matching the query.
func (b *Backend) Search(ctx context.Context, query string, limit int) ([]backend.Feature, error) {
	features, err := b.loadFeatures(ctx)
	if err != nil {
		return nil, err
	}

	return FilterFeatures(features, query, limit), nil
}

// GetFeature retrieves a feature by ID.
func (b *Backend) GetFeature(ctx context.Context, id string) (*backend.Feature, error) {
	if !IsValidFeatureID(id) {
		return nil, backend.ErrInvalidID
	}

	// First check cache - return a copy to prevent mutation of cached data
	b.mu.RLock()
	if b.cacheLoaded {
		for _, f := range b.cachedFeatures {
			if f.ID == id {
				// Make a copy to prevent callers from mutating cache
				featureCopy := f
				featureCopy.Tags = append([]string(nil), f.Tags...)
				b.mu.RUnlock()
				return &featureCopy, nil
			}
		}
	}
	b.mu.RUnlock()

	// Fetch directly from GitLab
	filePath := FeatureFilePath(id)
	content, err := b.getFileContent(ctx, filePath)
	if err != nil {
		return nil, err
	}

	return ParseFeatureFile(content)
}

// FeatureExists checks if a feature with the given ID exists.
func (b *Backend) FeatureExists(ctx context.Context, id string) (bool, error) {
	if !IsValidFeatureID(id) {
		return false, backend.ErrInvalidID
	}

	filePath := FeatureFilePath(id)
	_, err := b.getFileContent(ctx, filePath)
	if err != nil {
		if errors.Is(err, backend.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ListAll returns all features from the repository.
func (b *Backend) ListAll(ctx context.Context) ([]backend.Feature, error) {
	return b.loadFeatures(ctx)
}

// CreateFeature creates a new feature via MR workflow.
// The function:
// 1. Assigns a new ID (FT-NNNNNN) based on max existing
// 2. Creates a feature branch
// 3. Commits the feature YAML file
// 4. Creates a merge request
// Returns the feature with assigned ID and MR details in context.
func (b *Backend) CreateFeature(ctx context.Context, feature backend.Feature) (*backend.Feature, error) {
	// Validate required fields
	if feature.Name == "" {
		return nil, fmt.Errorf("%w: name is required", backend.ErrInvalidRequest)
	}

	// Assign new ID
	newFeature, err := b.assignNextID(ctx, feature)
	if err != nil {
		return nil, fmt.Errorf("assign ID: %w", err)
	}

	// Set timestamps
	now := time.Now().UTC()
	newFeature.CreatedAt = now
	newFeature.UpdatedAt = now

	// Attempt creation with retry for ID collision
	const maxRetries = 3
	var lastErr error

	for attempt := range maxRetries {
		if attempt > 0 {
			// Re-assign ID on retry (in case of collision)
			b.InvalidateCache() // Clear cache to get fresh IDs
			newFeature, err = b.assignNextID(ctx, feature)
			if err != nil {
				return nil, fmt.Errorf("assign ID (retry %d): %w", attempt, err)
			}
			newFeature.CreatedAt = now
			newFeature.UpdatedAt = now
		}

		result, createErr := b.createFeatureWithMR(ctx, newFeature, OpCreate)
		if createErr == nil {
			// Success - invalidate cache since we added a new feature
			b.InvalidateCache()
			return result, nil
		}

		// Check if error is a conflict (file already exists)
		if errors.Is(createErr, backend.ErrAlreadyExists) || errors.Is(createErr, backend.ErrConflict) {
			lastErr = createErr
			continue // Retry with new ID
		}

		// Other errors are not retryable
		return nil, createErr
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// UpdateFeature updates an existing feature via MR workflow.
// The function:
// 1. Verifies the feature exists
// 2. Creates a feature branch
// 3. Updates the feature YAML file
// 4. Creates a merge request
func (b *Backend) UpdateFeature(ctx context.Context, id string, updates backend.Feature) (*backend.Feature, error) {
	if !IsValidFeatureID(id) {
		return nil, backend.ErrInvalidID
	}

	// Get existing feature
	existing, err := b.GetFeature(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates (keep existing values for empty fields)
	updated := *existing
	if updates.Name != "" {
		updated.Name = updates.Name
	}
	if updates.Summary != "" {
		updated.Summary = updates.Summary
	}
	if updates.Owner != "" {
		updated.Owner = updates.Owner
	}
	if len(updates.Tags) > 0 {
		updated.Tags = updates.Tags
	}
	updated.UpdatedAt = time.Now().UTC()

	// Create MR for update
	result, err := b.updateFeatureWithMR(ctx, &updated)
	if err != nil {
		return nil, err
	}

	// Invalidate cache
	b.InvalidateCache()
	return result, nil
}

// DeleteFeature deletes a feature via MR workflow.
// The function:
// 1. Verifies the feature exists
// 2. Creates a feature branch
// 3. Deletes the feature YAML file
// 4. Creates a merge request
func (b *Backend) DeleteFeature(ctx context.Context, id string) error {
	if !IsValidFeatureID(id) {
		return backend.ErrInvalidID
	}

	// Get existing feature (to verify it exists and for MR description)
	existing, err := b.GetFeature(ctx, id)
	if err != nil {
		return err
	}

	// Create MR for deletion
	if err := b.deleteFeatureWithMR(ctx, existing); err != nil {
		return err
	}

	// Invalidate cache
	b.InvalidateCache()
	return nil
}

// createFeatureWithMR handles the branch/commit/MR workflow for creating a feature.
func (b *Backend) createFeatureWithMR(ctx context.Context, feature *backend.Feature, op string) (*backend.Feature, error) {
	// Generate branch name
	branch := BranchName(op, feature)

	// Create branch
	if branchErr := b.createBranch(ctx, branch); branchErr != nil {
		return nil, fmt.Errorf("create branch: %w", branchErr)
	}

	// Cleanup branch on failure using a separate function to satisfy contextcheck
	cleanupBranch := true
	cleanup := func() {
		if cleanupBranch {
			// Best effort cleanup - ignore errors
			//nolint:errcheck // Best effort cleanup
			b.deleteBranch(ctx, branch)
		}
	}
	defer cleanup()

	// Format feature file
	content, formatErr := FormatFeatureFile(feature)
	if formatErr != nil {
		return nil, fmt.Errorf("format feature file: %w", formatErr)
	}

	// Commit file
	filePath := FeatureFilePath(feature.ID)
	commitMsg := CommitMessage(op, feature)
	if commitErr := b.commitFile(ctx, branch, filePath, string(content), commitMsg); commitErr != nil {
		// Check if file already exists
		if errors.Is(commitErr, backend.ErrConflict) {
			return nil, backend.ErrAlreadyExists
		}
		return nil, fmt.Errorf("commit file: %w", commitErr)
	}

	// Create MR
	title := MRTitle(op, feature)
	description := MRDescription(op, feature)
	mrInfo, mrErr := b.createMergeRequest(ctx, branch, title, description, b.mrConfig)
	if mrErr != nil {
		return nil, fmt.Errorf("create merge request: %w", mrErr)
	}

	// Track pending MR
	if trackErr := trackPendingMR(feature, mrInfo, op); trackErr != nil {
		// Non-fatal - log and continue
		// The MR was created successfully, just tracking failed
		_ = trackErr
	}

	// Success - don't cleanup branch
	cleanupBranch = false

	return feature, nil
}

// updateFeatureWithMR handles the branch/commit/MR workflow for updating a feature.
func (b *Backend) updateFeatureWithMR(ctx context.Context, feature *backend.Feature) (*backend.Feature, error) {
	// Generate branch name
	branch := BranchName(OpUpdate, feature)

	// Create branch
	if branchErr := b.createBranch(ctx, branch); branchErr != nil {
		return nil, fmt.Errorf("create branch: %w", branchErr)
	}

	// Cleanup branch on failure
	cleanupBranch := true
	cleanup := func() {
		if cleanupBranch {
			//nolint:errcheck // Best effort cleanup
			b.deleteBranch(ctx, branch)
		}
	}
	defer cleanup()

	// Format feature file
	content, formatErr := FormatFeatureFile(feature)
	if formatErr != nil {
		return nil, fmt.Errorf("format feature file: %w", formatErr)
	}

	// Update file
	filePath := FeatureFilePath(feature.ID)
	commitMsg := CommitMessage(OpUpdate, feature)
	if updateErr := b.updateFile(ctx, branch, filePath, string(content), commitMsg); updateErr != nil {
		return nil, fmt.Errorf("update file: %w", updateErr)
	}

	// Create MR
	title := MRTitle(OpUpdate, feature)
	description := MRDescription(OpUpdate, feature)
	mrInfo, mrErr := b.createMergeRequest(ctx, branch, title, description, b.mrConfig)
	if mrErr != nil {
		return nil, fmt.Errorf("create merge request: %w", mrErr)
	}

	// Track pending MR
	if trackErr := trackPendingMR(feature, mrInfo, OpUpdate); trackErr != nil {
		_ = trackErr // Non-fatal
	}

	// Success
	cleanupBranch = false
	return feature, nil
}

// deleteFeatureWithMR handles the branch/commit/MR workflow for deleting a feature.
func (b *Backend) deleteFeatureWithMR(ctx context.Context, feature *backend.Feature) error {
	// Generate branch name
	branch := BranchName(OpDelete, feature)

	// Create branch
	if branchErr := b.createBranch(ctx, branch); branchErr != nil {
		return fmt.Errorf("create branch: %w", branchErr)
	}

	// Cleanup branch on failure
	cleanupBranch := true
	cleanup := func() {
		if cleanupBranch {
			//nolint:errcheck // Best effort cleanup
			b.deleteBranch(ctx, branch)
		}
	}
	defer cleanup()

	// Delete file
	filePath := FeatureFilePath(feature.ID)
	commitMsg := CommitMessage(OpDelete, feature)
	if deleteErr := b.deleteFile(ctx, branch, filePath, commitMsg); deleteErr != nil {
		return fmt.Errorf("delete file: %w", deleteErr)
	}

	// Create MR
	title := MRTitle(OpDelete, feature)
	description := MRDescription(OpDelete, feature)
	mrInfo, mrErr := b.createMergeRequest(ctx, branch, title, description, b.mrConfig)
	if mrErr != nil {
		return fmt.Errorf("create merge request: %w", mrErr)
	}

	// Track pending MR
	if trackErr := trackPendingMR(feature, mrInfo, OpDelete); trackErr != nil {
		_ = trackErr // Non-fatal
	}

	// Success
	cleanupBranch = false
	return nil
}

// GetAuthInfo returns the authenticated user's information.
func (b *Backend) GetAuthInfo(ctx context.Context) (*backend.AuthInfo, error) {
	user, resp, err := b.client.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil {
		return nil, mapError(resp, err)
	}

	// Get project membership for role
	role := "guest"
	member, _, err := b.client.ProjectMembers.GetProjectMember(b.project, user.ID, gitlab.WithContext(ctx))
	if err == nil && member != nil {
		role = accessLevelToRole(member.AccessLevel)
	}
	// Note: 404 (user is not a direct member) is ignored - user might have access through group membership

	return &backend.AuthInfo{
		Username:    user.Username,
		DisplayName: user.Name,
		Role:        role,
	}, nil
}

// loadFeatures loads all features from the repository, using cache if available.
func (b *Backend) loadFeatures(ctx context.Context) ([]backend.Feature, error) {
	// Check cache
	b.mu.RLock()
	if b.cacheLoaded {
		features := make([]backend.Feature, len(b.cachedFeatures))
		copy(features, b.cachedFeatures)
		b.mu.RUnlock()
		return features, nil
	}
	b.mu.RUnlock()

	// Load from GitLab
	b.mu.Lock()
	defer b.mu.Unlock()

	// Double-check after acquiring write lock
	if b.cacheLoaded {
		features := make([]backend.Feature, len(b.cachedFeatures))
		copy(features, b.cachedFeatures)
		return features, nil
	}

	// List files in features directory
	opts := &gitlab.ListTreeOptions{
		Path:      gitlab.Ptr(FeaturesDir),
		Ref:       gitlab.Ptr(b.mainBranch),
		Recursive: gitlab.Ptr(false),
	}

	var allFeatures []backend.Feature
	var treeNodes []*gitlab.TreeNode

	// Paginate through all files with retry logic
	retryCfg := DefaultRetryConfig()
	for {
		var nodes []*gitlab.TreeNode
		var nextPage int64
		var notFound bool

		listErr := WithRetry(ctx, retryCfg, func() (*gitlab.Response, error) {
			var resp *gitlab.Response
			var err error
			nodes, resp, err = b.client.Repositories.ListTree(b.project, opts, gitlab.WithContext(ctx))
			if err != nil {
				if resp != nil && resp.StatusCode == http.StatusNotFound {
					notFound = true
					return resp, nil // Don't retry 404
				}
				return resp, err
			}
			if resp != nil {
				nextPage = resp.NextPage
			}
			return resp, nil
		})

		if notFound {
			// features/ directory doesn't exist yet - return empty
			b.cachedFeatures = []backend.Feature{}
			b.cacheLoaded = true
			return b.cachedFeatures, nil
		}

		if listErr != nil {
			return nil, mapErrorFromErr(listErr)
		}

		treeNodes = append(treeNodes, nodes...)

		if nextPage == 0 {
			break
		}
		opts.Page = nextPage
	}

	// Fetch each feature file
	for _, node := range treeNodes {
		if node.Type != "blob" {
			continue
		}

		id := FeatureIDFromPath(node.Path)
		if id == "" {
			continue // Not a valid feature file
		}

		content, err := b.getFileContent(ctx, node.Path)
		if err != nil {
			// Skip files that can't be read
			continue
		}

		feature, err := ParseFeatureFile(content)
		if err != nil {
			// Skip files that can't be parsed
			continue
		}

		allFeatures = append(allFeatures, *feature)
	}

	b.cachedFeatures = allFeatures
	b.cacheLoaded = true

	return allFeatures, nil
}

// getFileContent retrieves a file's content from the repository.
func (b *Backend) getFileContent(ctx context.Context, path string) ([]byte, error) {
	opts := &gitlab.GetFileOptions{
		Ref: gitlab.Ptr(b.mainBranch),
	}

	// Use retry logic for resilience against transient failures
	file, err := WithRetryResult(ctx, DefaultRetryConfig(), func() (*gitlab.File, *gitlab.Response, error) {
		return b.client.RepositoryFiles.GetFile(b.project, path, opts, gitlab.WithContext(ctx))
	})
	if err != nil {
		// Map error after retries exhausted
		return nil, mapErrorFromErr(err)
	}

	// Decode base64 content
	content, decodeErr := base64.StdEncoding.DecodeString(file.Content)
	if decodeErr != nil {
		return nil, fmt.Errorf("decode file content: %w", decodeErr)
	}

	return content, nil
}

// InvalidateCache clears the feature cache, forcing a reload on next access.
func (b *Backend) InvalidateCache() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cachedFeatures = nil
	b.cacheLoaded = false
}

// mapError converts GitLab API errors to backend errors.
func mapError(resp *gitlab.Response, err error) error {
	if err == nil {
		return nil
	}

	// Check HTTP status code if response is available
	if resp != nil {
		switch resp.StatusCode {
		case http.StatusNotFound:
			return backend.ErrNotFound
		case http.StatusForbidden, http.StatusUnauthorized:
			return fmt.Errorf("%w: %w", backend.ErrPermission, err)
		case http.StatusConflict:
			return fmt.Errorf("%w: %w", backend.ErrConflict, err)
		case http.StatusTooManyRequests:
			return fmt.Errorf("%w: %w", backend.ErrRateLimited, err)
		case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
			return fmt.Errorf("%w: %w", backend.ErrBackendOffline, err)
		}
	}

	// Check error message patterns
	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "connection refused"):
		return fmt.Errorf("%w: %w", backend.ErrBackendOffline, err)
	case strings.Contains(errMsg, "no such host"):
		return fmt.Errorf("%w: %w", backend.ErrBackendOffline, err)
	case strings.Contains(errMsg, "timeout"):
		return fmt.Errorf("%w: %w", backend.ErrBackendOffline, err)
	case strings.Contains(errMsg, "404"):
		return backend.ErrNotFound
	case strings.Contains(errMsg, "403") || strings.Contains(errMsg, "401"):
		return fmt.Errorf("%w: %w", backend.ErrPermission, err)
	}

	return err
}

// mapErrorFromErr converts errors to backend errors without a response.
// Used when the response is not available (e.g., after retry exhaustion).
func mapErrorFromErr(err error) error {
	if err == nil {
		return nil
	}
	// Delegate to mapError with nil response - it handles error message patterns
	return mapError(nil, err)
}

// accessLevelToRole converts GitLab access levels to role strings.
func accessLevelToRole(level gitlab.AccessLevelValue) string {
	switch level {
	case gitlab.OwnerPermissions:
		return "owner"
	case gitlab.MaintainerPermissions:
		return "maintainer"
	case gitlab.DeveloperPermissions:
		return "developer"
	case gitlab.ReporterPermissions:
		return "reporter"
	case gitlab.GuestPermissions:
		return "guest"
	case gitlab.NoPermissions, gitlab.MinimalAccessPermissions:
		return "none"
	case gitlab.PlannerPermissions:
		return "planner"
	case gitlab.AdminPermissions:
		return "admin"
	default:
		return "unknown"
	}
}
