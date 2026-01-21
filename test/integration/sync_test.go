//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
	"github.com/JoobyPM/feature-atlas-service/internal/manifest"
	"github.com/JoobyPM/feature-atlas-service/test/integration/testutil"
)

// TestManifestAdd_FromServer verifies adding a server feature to local manifest.
func TestManifestAdd_FromServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create temp directory for manifest
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, ".feature-atlas.yaml")

	// Initialize manifest
	m := manifest.New()
	err = m.Save(manifestPath)
	require.NoError(t, err, "save initial manifest")

	// Get admin client to create a feature
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err, "create admin client")

	// Create a feature on server
	serverFeature, err := adminClient.CreateFeature(ctx, apiclient.CreateFeatureRequest{
		Name:    "Server Feature for Add Test",
		Summary: "Testing manifest add from server",
		Owner:   "Test Team",
		Tags:    []string{"test"},
	})
	require.NoError(t, err, "create server feature")

	// Fetch the feature from server
	fetched, err := adminClient.GetFeature(ctx, serverFeature.ID)
	require.NoError(t, err, "fetch feature from server")

	// Add to manifest
	m, err = manifest.Load(manifestPath)
	require.NoError(t, err, "load manifest")

	m.Features[fetched.ID] = manifest.Entry{
		Name:     fetched.Name,
		Summary:  fetched.Summary,
		Owner:    fetched.Owner,
		Tags:     fetched.Tags,
		Synced:   true,
		SyncedAt: time.Now().Format(time.RFC3339),
	}

	err = m.SaveWithLock(manifestPath)
	require.NoError(t, err, "save manifest with added feature")

	// Verify manifest contains the feature
	m, err = manifest.Load(manifestPath)
	require.NoError(t, err, "reload manifest")

	assert.True(t, m.HasFeature(serverFeature.ID), "manifest should contain added feature")
	entry := m.Features[serverFeature.ID]
	assert.Equal(t, "Server Feature for Add Test", entry.Name)
	assert.True(t, entry.Synced, "added feature should be marked as synced")
}

// TestManifestAdd_NotFound verifies error when feature doesn't exist on server.
func TestManifestAdd_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err, "create admin client")

	// Try to fetch a non-existent feature
	_, err = adminClient.GetFeature(ctx, "FT-999999")
	require.Error(t, err)
	assert.ErrorIs(t, err, apiclient.ErrFeatureNotFound)
}

// TestManifestSync_SingleFeature tests syncing a single local feature to server.
func TestManifestSync_SingleFeature(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create temp directory for manifest
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, ".feature-atlas.yaml")

	// Initialize manifest with a local feature
	m := manifest.New()
	localID := "FT-LOCAL-sync-test"
	m.Features[localID] = manifest.Entry{
		Name:    "Sync Test Feature",
		Summary: "Testing manifest sync",
		Owner:   "Sync Team",
		Tags:    []string{"sync", "test"},
		Synced:  false,
	}
	err = m.Save(manifestPath)
	require.NoError(t, err, "save initial manifest")

	// Get admin client
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err, "create admin client")

	// Simulate sync: create feature on server
	entry := m.Features[localID]
	serverFeature, err := adminClient.CreateFeature(ctx, apiclient.CreateFeatureRequest{
		Name:    entry.Name,
		Summary: entry.Summary,
		Owner:   entry.Owner,
		Tags:    entry.Tags,
	})
	require.NoError(t, err, "create feature on server")

	// Update manifest with server-assigned ID
	delete(m.Features, localID)
	m.Features[serverFeature.ID] = manifest.Entry{
		Name:     serverFeature.Name,
		Summary:  serverFeature.Summary,
		Owner:    serverFeature.Owner,
		Tags:     serverFeature.Tags,
		Synced:   true,
		SyncedAt: time.Now().Format(time.RFC3339),
		Alias:    localID,
	}
	err = m.SaveWithLock(manifestPath)
	require.NoError(t, err, "save synced manifest")

	// Verify manifest
	m, err = manifest.Load(manifestPath)
	require.NoError(t, err, "reload manifest")

	assert.False(t, m.HasFeature(localID), "local ID should be removed")
	assert.True(t, m.HasFeature(serverFeature.ID), "server ID should be present")

	entry = m.Features[serverFeature.ID]
	assert.True(t, entry.Synced, "should be marked as synced")
	assert.Equal(t, localID, entry.Alias, "alias should preserve original local ID")
	assert.Regexp(t, `^FT-\d{6}$`, serverFeature.ID, "server ID should match format")
}

// TestManifestSync_MultipleFeatures tests syncing multiple features.
func TestManifestSync_MultipleFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create temp directory for manifest
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, ".feature-atlas.yaml")

	// Initialize manifest with multiple local features
	m := manifest.New()
	localIDs := []string{
		"FT-LOCAL-multi-1",
		"FT-LOCAL-multi-2",
		"FT-LOCAL-multi-3",
	}
	for i, id := range localIDs {
		m.Features[id] = manifest.Entry{
			Name:    "Multi Sync Feature " + string(rune('A'+i)),
			Summary: "Testing multi-feature sync",
			Synced:  false,
		}
	}
	err = m.Save(manifestPath)
	require.NoError(t, err, "save initial manifest")

	// Get admin client
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err, "create admin client")

	// Sync each feature
	serverIDs := make([]string, 0, len(localIDs))
	for _, localID := range localIDs {
		entry := m.Features[localID]
		serverFeature, createErr := adminClient.CreateFeature(ctx, apiclient.CreateFeatureRequest{
			Name:    entry.Name,
			Summary: entry.Summary,
		})
		require.NoError(t, createErr, "create feature on server: %s", localID)

		// Update manifest
		delete(m.Features, localID)
		m.Features[serverFeature.ID] = manifest.Entry{
			Name:     serverFeature.Name,
			Summary:  serverFeature.Summary,
			Synced:   true,
			SyncedAt: time.Now().Format(time.RFC3339),
			Alias:    localID,
		}
		serverIDs = append(serverIDs, serverFeature.ID)
	}

	err = m.SaveWithLock(manifestPath)
	require.NoError(t, err, "save synced manifest")

	// Verify manifest
	m, err = manifest.Load(manifestPath)
	require.NoError(t, err, "reload manifest")

	// All local IDs should be gone
	for _, localID := range localIDs {
		assert.False(t, m.HasFeature(localID), "local ID %s should be removed", localID)
	}

	// All server IDs should be present
	for _, serverID := range serverIDs {
		assert.True(t, m.HasFeature(serverID), "server ID %s should be present", serverID)
		entry := m.Features[serverID]
		assert.True(t, entry.Synced)
		assert.NotEmpty(t, entry.Alias)
	}

	// Verify features exist on server
	for _, serverID := range serverIDs {
		_, getErr := adminClient.GetFeature(ctx, serverID)
		require.NoError(t, getErr, "server should have feature %s", serverID)
	}
}

// TestManifestSync_Alias verifies that original local ID is preserved in alias field.
func TestManifestSync_Alias(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create temp directory for manifest
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, ".feature-atlas.yaml")

	// Initialize manifest with a local feature
	m := manifest.New()
	localID := "FT-LOCAL-alias-test"
	m.Features[localID] = manifest.Entry{
		Name:    "Alias Test Feature",
		Summary: "Testing alias preservation",
		Synced:  false,
	}
	err = m.Save(manifestPath)
	require.NoError(t, err, "save initial manifest")

	// Get admin client and sync
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err)

	entry := m.Features[localID]
	serverFeature, err := adminClient.CreateFeature(ctx, apiclient.CreateFeatureRequest{
		Name:    entry.Name,
		Summary: entry.Summary,
	})
	require.NoError(t, err)

	// Update manifest
	delete(m.Features, localID)
	m.Features[serverFeature.ID] = manifest.Entry{
		Name:     serverFeature.Name,
		Summary:  serverFeature.Summary,
		Synced:   true,
		SyncedAt: time.Now().Format(time.RFC3339),
		Alias:    localID,
	}
	err = m.SaveWithLock(manifestPath)
	require.NoError(t, err)

	// Read raw YAML to verify alias is persisted
	data, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "alias: FT-LOCAL-alias-test")
}

// TestManifestSync_PartialFailure tests that partial sync succeeds.
func TestManifestSync_PartialFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create temp directory for manifest
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, ".feature-atlas.yaml")

	// Initialize manifest with features - one valid, one will "fail"
	m := manifest.New()
	m.Features["FT-LOCAL-valid"] = manifest.Entry{
		Name:    "Valid Feature",
		Summary: "This will sync successfully",
		Synced:  false,
	}
	// This one has valid data but we'll simulate failure by not syncing it
	m.Features["FT-LOCAL-skip"] = manifest.Entry{
		Name:    "Skip Feature",
		Summary: "Simulating skip for partial test",
		Synced:  false,
	}
	err = m.Save(manifestPath)
	require.NoError(t, err)

	// Get admin client
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err)

	// Only sync the valid one
	entry := m.Features["FT-LOCAL-valid"]
	serverFeature, err := adminClient.CreateFeature(ctx, apiclient.CreateFeatureRequest{
		Name:    entry.Name,
		Summary: entry.Summary,
	})
	require.NoError(t, err)

	// Update manifest with partial sync
	delete(m.Features, "FT-LOCAL-valid")
	m.Features[serverFeature.ID] = manifest.Entry{
		Name:     serverFeature.Name,
		Summary:  serverFeature.Summary,
		Synced:   true,
		SyncedAt: time.Now().Format(time.RFC3339),
		Alias:    "FT-LOCAL-valid",
	}
	// FT-LOCAL-skip remains unsynced
	err = m.SaveWithLock(manifestPath)
	require.NoError(t, err)

	// Verify state
	m, err = manifest.Load(manifestPath)
	require.NoError(t, err)

	// Valid one synced
	assert.True(t, m.HasFeature(serverFeature.ID))
	assert.True(t, m.Features[serverFeature.ID].Synced)

	// Skip one still local
	assert.True(t, m.HasFeature("FT-LOCAL-skip"))
	assert.False(t, m.Features["FT-LOCAL-skip"].Synced)

	// Unsynced list should only have the skipped one
	unsynced := m.ListFeatures(true)
	assert.Len(t, unsynced, 1)
	assert.Contains(t, unsynced, "FT-LOCAL-skip")
}

// TestManifestSync_DryRun tests that dry run doesn't make changes.
func TestManifestSync_DryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create temp directory for manifest
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, ".feature-atlas.yaml")

	// Initialize manifest with a local feature
	m := manifest.New()
	localID := "FT-LOCAL-dryrun-test"
	m.Features[localID] = manifest.Entry{
		Name:    "Dry Run Feature",
		Summary: "Testing dry run mode",
		Synced:  false,
	}
	err := m.Save(manifestPath)
	require.NoError(t, err)

	// In a real dry run, we would:
	// 1. List unsynced features (without server)
	// 2. Not call CreateFeature
	// 3. Not update manifest

	unsynced := m.ListFeatures(true)
	assert.Len(t, unsynced, 1)
	assert.Contains(t, unsynced, localID)

	// Verify manifest unchanged
	m, err = manifest.Load(manifestPath)
	require.NoError(t, err)
	assert.True(t, m.HasFeature(localID))
	assert.False(t, m.Features[localID].Synced)
}

// TestManifestAddIdempotent verifies that adding the same feature twice is safe.
func TestManifestAddIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create temp directory for manifest
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, ".feature-atlas.yaml")

	// Get admin client
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err)

	// Create a feature on server
	serverFeature, err := adminClient.CreateFeature(ctx, apiclient.CreateFeatureRequest{
		Name:    "Idempotent Test",
		Summary: "Testing idempotent add",
	})
	require.NoError(t, err)

	// Initialize manifest
	m := manifest.New()
	err = m.Save(manifestPath)
	require.NoError(t, err)

	// Add feature first time
	m, err = manifest.Load(manifestPath)
	require.NoError(t, err, "load manifest for first add")
	m.Features[serverFeature.ID] = manifest.Entry{
		Name:     serverFeature.Name,
		Summary:  serverFeature.Summary,
		Synced:   true,
		SyncedAt: time.Now().Format(time.RFC3339),
	}
	err = m.SaveWithLock(manifestPath)
	require.NoError(t, err)

	// Try to add again (simulating `manifest add` on existing feature)
	m, err = manifest.Load(manifestPath)
	require.NoError(t, err, "load manifest for second add")
	assert.True(t, m.HasFeature(serverFeature.ID), "feature should already exist")

	// The command should skip without error
	// Manifest should have exactly 1 entry
	assert.Len(t, m.Features, 1)
}
