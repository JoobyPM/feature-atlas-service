package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_Load_Empty(t *testing.T) {
	// Cache that doesn't exist should load without error
	dir := t.TempDir()
	c := New(filepath.Join(dir, ".fas"))

	err := c.Load()
	assert.NoError(t, err, "loading non-existent cache should succeed")
	assert.False(t, c.HasData(), "cache should have no data")
	assert.True(t, c.IsStale(), "empty cache should be stale")
}

func TestCache_Save_Load(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".fas")
	c := New(cacheDir)

	// Add data
	c.Update([]CachedFeature{
		{ID: "FT-000001", Name: "Auth Service", Summary: "Authentication"},
		{ID: "FT-000002", Name: "Payment", Summary: "Payment processing"},
	}, "https://example.com", true)

	// Save
	err := c.Save()
	require.NoError(t, err)

	// Verify files exist
	assert.FileExists(t, filepath.Join(cacheDir, FeaturesFile))
	assert.FileExists(t, filepath.Join(cacheDir, MetaFile))

	// Load in new instance
	c2 := New(cacheDir)
	err = c2.Load()
	require.NoError(t, err)

	assert.True(t, c2.HasData())
	assert.Equal(t, 2, c2.FeatureCount())
	assert.True(t, c2.IsComplete())
}

func TestCache_Load_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".fas")
	require.NoError(t, os.MkdirAll(cacheDir, 0o750))

	// Write corrupt JSON
	err := os.WriteFile(filepath.Join(cacheDir, FeaturesFile), []byte("{invalid"), 0o600)
	require.NoError(t, err)

	c := New(cacheDir)
	err = c.Load()
	assert.Error(t, err, "loading corrupt cache should fail")
	assert.Contains(t, err.Error(), "corrupt features cache")
}

func TestCache_IsStale(t *testing.T) {
	dir := t.TempDir()
	c := New(filepath.Join(dir, ".fas"))

	// Empty cache is stale
	assert.True(t, c.IsStale(), "empty cache should be stale")

	// Fresh cache is not stale
	c.Update([]CachedFeature{}, "https://example.com", false)
	assert.False(t, c.IsStale(), "fresh cache should not be stale")

	// Manually set old timestamp
	c.mu.Lock()
	c.meta.LastSync = time.Now().Add(-2 * time.Hour)
	c.mu.Unlock()

	assert.True(t, c.IsStale(), "old cache should be stale")
}

func TestCache_IsComplete(t *testing.T) {
	dir := t.TempDir()
	c := New(filepath.Join(dir, ".fas"))

	// Empty cache is not complete
	assert.False(t, c.IsComplete())

	// Incomplete cache
	c.Update([]CachedFeature{}, "https://example.com", false)
	assert.False(t, c.IsComplete())

	// Complete cache
	c.Update([]CachedFeature{}, "https://example.com", true)
	assert.True(t, c.IsComplete())
}

func TestCache_FindByNameExact(t *testing.T) {
	c := New(t.TempDir())
	c.Update([]CachedFeature{
		{ID: "FT-000001", Name: "Authentication Service", Summary: "Auth"},
		{ID: "FT-000002", Name: "Payment Gateway", Summary: "Payments"},
		{ID: "FT-000003", Name: "User Management", Summary: "Users"},
	}, "https://example.com", true)

	tests := []struct {
		name      string
		search    string
		expectID  string
		expectNil bool
	}{
		{"exact match", "Authentication Service", "FT-000001", false},
		{"case insensitive", "authentication service", "FT-000001", false},
		{"mixed case", "AuThEnTiCaTiOn SeRvIcE", "FT-000001", false},
		{"partial match returns nil", "Authentication", "", true},
		{"not found", "Nonexistent", "", true},
		{"empty search", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.FindByNameExact(tt.search)
			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expectID, result.ID)
			}
		})
	}
}

func TestCache_Update(t *testing.T) {
	c := New(t.TempDir())

	features := []CachedFeature{
		{ID: "FT-000001", Name: "Test 1", Summary: "Summary 1"},
		{ID: "FT-000002", Name: "Test 2", Summary: "Summary 2"},
	}

	c.Update(features, "https://api.example.com", true)

	assert.True(t, c.HasData())
	assert.Equal(t, 2, c.FeatureCount())
	assert.True(t, c.IsComplete())
	assert.False(t, c.IsStale())
}

func TestCache_Add(t *testing.T) {
	c := New(t.TempDir())

	// Add to empty cache
	c.Add(CachedFeature{ID: "FT-000001", Name: "First", Summary: "First feature"})
	assert.Equal(t, 1, c.FeatureCount())

	// Add another
	c.Add(CachedFeature{ID: "FT-000002", Name: "Second", Summary: "Second feature"})
	assert.Equal(t, 2, c.FeatureCount())

	// Verify they're findable
	assert.NotNil(t, c.FindByNameExact("First"))
	assert.NotNil(t, c.FindByNameExact("Second"))
}

func TestCache_Add_WithMeta(t *testing.T) {
	c := New(t.TempDir())

	// Initialize with Update first
	c.Update([]CachedFeature{
		{ID: "FT-000001", Name: "First", Summary: "First feature"},
	}, "https://example.com", false)
	assert.Equal(t, 1, c.FeatureCount())

	// Add increments meta count
	c.Add(CachedFeature{ID: "FT-000002", Name: "Second", Summary: "Second feature"})

	c.mu.RLock()
	count := c.meta.FeatureCount
	c.mu.RUnlock()

	assert.Equal(t, 2, count, "meta.FeatureCount should be incremented")
}

func TestCache_ThreadSafety(t *testing.T) {
	c := New(t.TempDir())
	c.Update([]CachedFeature{
		{ID: "FT-000001", Name: "Test", Summary: "Test feature"},
	}, "https://example.com", true)

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent reads
	for range iterations {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.IsStale()
			_ = c.IsComplete()
			_ = c.HasData()
			_ = c.FeatureCount()
			_ = c.FindByNameExact("Test")
		}()
	}

	// Concurrent adds
	for idx := range iterations {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c.Add(CachedFeature{
				ID:      "FT-" + string(rune('A'+idx%26)),
				Name:    "Concurrent",
				Summary: "Test",
			})
		}(idx)
	}

	wg.Wait()
	// If we get here without a race, test passes
	assert.True(t, c.FeatureCount() > 1)
}

func TestCache_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".fas")
	c := New(cacheDir)

	c.Update([]CachedFeature{
		{ID: "FT-000001", Name: "Test Feature", Summary: "A test"},
	}, "https://api.example.com", true)

	require.NoError(t, c.Save())

	// Read and verify features.json format
	data, err := os.ReadFile(filepath.Join(cacheDir, FeaturesFile)) //nolint:gosec // Test code
	require.NoError(t, err)

	var features CachedFeatures
	require.NoError(t, json.Unmarshal(data, &features))
	assert.Equal(t, "1", features.Version)
	assert.Len(t, features.Features, 1)
	assert.Equal(t, "FT-000001", features.Features[0].ID)

	// Read and verify meta.json format
	data, err = os.ReadFile(filepath.Join(cacheDir, MetaFile)) //nolint:gosec // Test code
	require.NoError(t, err)

	var meta Meta
	require.NoError(t, json.Unmarshal(data, &meta))
	assert.Equal(t, "1", meta.Version)
	assert.Equal(t, "https://api.example.com", meta.ServerURL)
	assert.Equal(t, 1, meta.FeatureCount)
	assert.True(t, meta.IsComplete)
	assert.Equal(t, int(DefaultTTL.Seconds()), meta.TTLSeconds)
}

func TestCache_ResolveDir_CWD(t *testing.T) {
	// When not in git and no manifest, should fall back to CWD
	// This test uses a temp dir that's not a git repo
	tmpDir := t.TempDir()

	// Change to temp dir
	original, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		_ = os.Chdir(original)
	}()
	require.NoError(t, os.Chdir(tmpDir))

	dir, err := ResolveDir()
	require.NoError(t, err)

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	expected, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	actual, err := filepath.EvalSymlinks(filepath.Dir(dir))
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(expected, DirName), filepath.Join(actual, DirName))
}

func TestCache_Dir(t *testing.T) {
	expectedDir := "/some/path/.fas"
	c := New(expectedDir)
	assert.Equal(t, expectedDir, c.Dir())
}
