//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
	"github.com/JoobyPM/feature-atlas-service/internal/backend/gitlab"
	"github.com/JoobyPM/feature-atlas-service/internal/config"
	"github.com/JoobyPM/feature-atlas-service/test/integration/testutil"
)

// TestGitLabBackend_ListAll verifies listing all features from GitLab.
func TestGitLabBackendListAll(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create mock GitLab server
	mockServer := testutil.NewGitLabMockServer(5)
	defer mockServer.Close()

	// Create backend
	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	backend, err := gitlab.New(cfg)
	require.NoError(t, err, "create GitLab backend")

	// List all features
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	features, err := backend.ListAll(ctx)
	require.NoError(t, err, "list all features")
	assert.Len(t, features, 5, "should have 5 seeded features")

	// Verify features have expected fields
	for _, f := range features {
		assert.Regexp(t, `^FT-\d{6}$`, f.ID, "feature ID should match format")
		assert.NotEmpty(t, f.Name, "feature should have name")
		assert.NotEmpty(t, f.Summary, "feature should have summary")
	}
}

// TestGitLabBackend_GetFeature verifies getting a specific feature.
func TestGitLabBackendGetFeature(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockServer := testutil.NewGitLabMockServer(5)
	defer mockServer.Close()

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	backend, err := gitlab.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get existing feature
	feature, err := backend.GetFeature(ctx, "FT-000001")
	require.NoError(t, err, "get feature")
	assert.Equal(t, "FT-000001", feature.ID)
	assert.Equal(t, "Feature 1", feature.Name)
}

// TestGitLabBackend_GetFeature_NotFound verifies 404 for non-existent feature.
func TestGitLabBackendGetFeatureNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockServer := testutil.NewGitLabMockServer(5)
	defer mockServer.Close()

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	glBackend, err := gitlab.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = glBackend.GetFeature(ctx, "FT-999999")
	require.Error(t, err)
	assert.ErrorIs(t, err, backend.ErrNotFound)
}

// TestGitLabBackend_Search verifies search functionality.
func TestGitLabBackendSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockServer := testutil.NewGitLabMockServer(10)
	defer mockServer.Close()

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	backend, err := gitlab.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Search with query
	features, err := backend.Search(ctx, "Feature", 5)
	require.NoError(t, err, "search features")
	assert.LessOrEqual(t, len(features), 5, "should respect limit")
}

// TestGitLabBackend_Suggest verifies autocomplete functionality.
func TestGitLabBackendSuggest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockServer := testutil.NewGitLabMockServer(10)
	defer mockServer.Close()

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	backend, err := gitlab.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get suggestions
	suggestions, err := backend.Suggest(ctx, "Feat", 5)
	require.NoError(t, err, "get suggestions")
	assert.LessOrEqual(t, len(suggestions), 5, "should respect limit")

	// Verify suggestion format
	for _, s := range suggestions {
		assert.NotEmpty(t, s.ID, "suggestion should have ID")
		assert.NotEmpty(t, s.Name, "suggestion should have name")
	}
}

// TestGitLabBackend_FeatureExists verifies existence check.
func TestGitLabBackendFeatureExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockServer := testutil.NewGitLabMockServer(5)
	defer mockServer.Close()

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	backend, err := gitlab.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Existing feature
	exists, err := backend.FeatureExists(ctx, "FT-000001")
	require.NoError(t, err)
	assert.True(t, exists, "seeded feature should exist")

	// Non-existent feature
	exists, err = backend.FeatureExists(ctx, "FT-999999")
	require.NoError(t, err)
	assert.False(t, exists, "non-existent feature should not exist")
}

// TestGitLabBackend_GetAuthInfo verifies auth info retrieval.
func TestGitLabBackendGetAuthInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockServer := testutil.NewGitLabMockServer(1)
	defer mockServer.Close()

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	backend, err := gitlab.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info, err := backend.GetAuthInfo(ctx)
	require.NoError(t, err, "get auth info")
	assert.Equal(t, "testuser", info.Username)
	assert.NotEmpty(t, info.Role)
}

// TestGitLabBackend_Mode verifies the backend mode.
func TestGitLabBackendMode(t *testing.T) {
	mockServer := testutil.NewGitLabMockServer(1)
	defer mockServer.Close()

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	backend, err := gitlab.New(cfg)
	require.NoError(t, err)

	assert.Equal(t, "gitlab", backend.Mode())
}

// TestGitLabBackend_CreateFeature verifies feature creation via MR.
func TestGitLabBackendCreateFeature(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockServer := testutil.NewGitLabMockServer(5)
	defer mockServer.Close()

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	glBackend, err := gitlab.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create feature
	feature := backend.Feature{
		Name:    "New Test Feature",
		Summary: "Created via integration test",
		Owner:   "Test Team",
		Tags:    []string{"test", "integration"},
	}

	created, err := glBackend.CreateFeature(ctx, feature)
	require.NoError(t, err, "create feature")
	assert.NotEmpty(t, created.ID, "should have assigned ID")
	assert.Regexp(t, `^FT-\d{6}$`, created.ID, "ID should match format")
	assert.Equal(t, "New Test Feature", created.Name)
}

// TestGitLabBackend_InvalidID verifies validation of feature IDs.
func TestGitLabBackendInvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockServer := testutil.NewGitLabMockServer(1)
	defer mockServer.Close()

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	glBackend, err := gitlab.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Invalid ID format
	_, err = glBackend.GetFeature(ctx, "INVALID")
	require.Error(t, err)
	assert.ErrorIs(t, err, backend.ErrInvalidID)
}

// TestGitLabBackend_Caching verifies that caching works correctly.
func TestGitLabBackendCaching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	mockServer := testutil.NewGitLabMockServer(5)
	defer mockServer.Close()

	callCount := 0
	mockServer.OnListFiles = func() error {
		callCount++
		return nil
	}

	cfg := config.GitLabConfig{
		Instance:   mockServer.URL(),
		Token:      "test-token",
		Project:    "test/project",
		MainBranch: "main",
	}
	backend, err := gitlab.New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First call - should hit server
	_, err = backend.ListAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "first call should hit server")

	// Second call - should use cache
	_, err = backend.ListAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "second call should use cache")

	// Invalidate cache
	backend.InvalidateCache()

	// Third call - should hit server again
	_, err = backend.ListAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "third call should hit server after invalidation")
}
