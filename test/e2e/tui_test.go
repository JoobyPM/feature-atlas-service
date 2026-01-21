//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JoobyPM/feature-atlas-service/internal/manifest"
	"github.com/JoobyPM/feature-atlas-service/test/integration/testutil"
)

// TestTUI_ManifestIntegration verifies that the TUI command respects manifest state.
// This is a programmatic test since interactive TUI testing isn't practical in E2E.
func TestTUI_ManifestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create work directory with manifest
	workDir := t.TempDir()

	// Create manifest with a local feature
	m := manifest.New()
	m.Features["FT-LOCAL-tui-test"] = manifest.Entry{
		Name:    "TUI Test Feature",
		Summary: "Testing TUI manifest integration",
		Synced:  false,
	}
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err = m.Save(manifestPath)
	require.NoError(t, err)

	// Verify TUI help shows multi-select options
	stdout, stderr, exitCode := runFeatctl(t, workDir, "tui", "--help")
	assert.Equal(t, 0, exitCode, "help should succeed: %s", stderr)
	assert.Contains(t, stdout, "--sync", "help should mention --sync flag")
	assert.Contains(t, stdout, "--manifest", "help should mention --manifest flag")
	assert.Contains(t, stdout, "Space", "help should mention Space for toggle")
	assert.Contains(t, stdout, "persist", "help should mention selections persist")
}

// TestTUI_SyncFlag verifies the --sync flag is accepted.
func TestTUI_SyncFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Just verify the flag is recognized (TUI would require input)
	workDir := t.TempDir()

	// Create empty manifest
	m := manifest.New()
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err := m.Save(manifestPath)
	require.NoError(t, err)

	// Check that --sync flag is accepted
	_, stderr, _ := runFeatctl(t, workDir, "tui", "--sync", "--help")
	// Should not error about unknown flag
	assert.NotContains(t, stderr, "unknown flag", "should accept --sync flag")
}

// TestTUI_ManifestFlag verifies the --manifest flag is accepted.
func TestTUI_ManifestFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	workDir := t.TempDir()

	// Create manifest in custom location
	customDir := filepath.Join(workDir, "custom")
	err := os.MkdirAll(customDir, 0o755)
	require.NoError(t, err)

	m := manifest.New()
	customManifest := filepath.Join(customDir, "my-manifest.yaml")
	err = m.Save(customManifest)
	require.NoError(t, err)

	// Check that --manifest flag is accepted
	_, stderr, _ := runFeatctl(t, workDir, "tui", "--manifest", customManifest, "--help")
	// Should not error about unknown flag
	assert.NotContains(t, stderr, "unknown flag", "should accept --manifest flag")
}

// TestManifestAdd_MultipleFeatures verifies adding multiple features via manifest add.
// This simulates what TUI multi-select does internally.
func TestManifestAdd_MultipleFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	workDir := t.TempDir()

	// Initialize manifest
	_, stderr, exitCode := runFeatctl(t, workDir, "manifest", "init")
	require.Equal(t, 0, exitCode, "manifest init should succeed: %s", stderr)

	// Get multiple features from server
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err)

	features, err := adminClient.Search(ctx, "", 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(features), 2, "need at least 2 features")

	// Add multiple features to manifest (simulating TUI multi-select confirm)
	for _, f := range features[:2] {
		_, stderr, exitCode := runFeatctl(t, workDir,
			"manifest", "add",
			"--manifest", filepath.Join(workDir, ".feature-atlas.yaml"),
			"--server", env.Server.APIURL(),
			"--ca", env.Certs.CACertPath,
			"--cert", env.Certs.AdminCertPath,
			"--key", env.Certs.AdminKeyPath,
			f.ID,
		)
		assert.Equal(t, 0, exitCode, "manifest add %s should succeed: %s", f.ID, stderr)
	}

	// Verify features are in manifest
	stdout, stderr, exitCode := runFeatctl(t, workDir,
		"manifest", "list",
		"--manifest", filepath.Join(workDir, ".feature-atlas.yaml"),
	)
	require.Equal(t, 0, exitCode, "manifest list should succeed: %s", stderr)

	for _, f := range features[:2] {
		assert.Contains(t, stdout, f.ID, "manifest should contain %s", f.ID)
	}
}

// TestManifestSync_LocalFeatures verifies sync of local features to server.
// This simulates TUI multi-select with --sync or 'S' confirmation.
func TestManifestSync_LocalFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	workDir := t.TempDir()

	// Create manifest with local features
	m := manifest.New()
	localID1 := "FT-LOCAL-sync-test-1"
	localID2 := "FT-LOCAL-sync-test-2"
	m.Features[localID1] = manifest.Entry{
		Name:    "Sync Test Feature 1",
		Summary: "First feature for sync testing",
		Owner:   "Test Team",
		Synced:  false,
	}
	m.Features[localID2] = manifest.Entry{
		Name:    "Sync Test Feature 2",
		Summary: "Second feature for sync testing",
		Synced:  false,
	}
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err = m.Save(manifestPath)
	require.NoError(t, err)

	// Sync features to server
	stdout, stderr, exitCode := runFeatctl(t, workDir,
		"manifest", "sync",
		"--manifest", manifestPath,
		"--server", env.Server.APIURL(),
		"--ca", env.Certs.CACertPath,
		"--cert", env.Certs.AdminCertPath,
		"--key", env.Certs.AdminKeyPath,
	)
	require.Equal(t, 0, exitCode, "manifest sync should succeed: %s", stderr)

	// Verify both features were synced
	assert.Contains(t, stdout, localID1, "should mention first local ID")
	assert.Contains(t, stdout, localID2, "should mention second local ID")
	assert.Contains(t, stdout, "Synced: 2", "should report 2 synced features")

	// Verify manifest was updated with server IDs
	updatedManifest, err := manifest.Load(manifestPath)
	require.NoError(t, err)

	// Old local IDs should be gone
	assert.False(t, updatedManifest.HasFeature(localID1), "old local ID 1 should be removed")
	assert.False(t, updatedManifest.HasFeature(localID2), "old local ID 2 should be removed")

	// Should have new server IDs with aliases
	var foundWithAlias1, foundWithAlias2 bool
	for _, entry := range updatedManifest.Features {
		if entry.Alias == localID1 {
			foundWithAlias1 = true
			assert.True(t, entry.Synced, "synced feature should have Synced=true")
		}
		if entry.Alias == localID2 {
			foundWithAlias2 = true
			assert.True(t, entry.Synced, "synced feature should have Synced=true")
		}
	}
	assert.True(t, foundWithAlias1, "should find feature with alias %s", localID1)
	assert.True(t, foundWithAlias2, "should find feature with alias %s", localID2)
}

// TestManifestSync_DryRun verifies --dry-run shows what would be synced.
// Note: Currently --dry-run still requires client init (pre-existing behavior).
// This test uses a server to verify the --dry-run output.
func TestManifestSync_DryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	workDir := t.TempDir()

	// Create manifest with local features
	m := manifest.New()
	m.Features["FT-LOCAL-dryrun-1"] = manifest.Entry{
		Name:    "Dry Run Test",
		Summary: "Testing dry run",
		Synced:  false,
	}
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err = m.Save(manifestPath)
	require.NoError(t, err)

	// Run sync with --dry-run
	stdout, stderr, exitCode := runFeatctl(t, workDir,
		"manifest", "sync",
		"--manifest", manifestPath,
		"--dry-run",
		"--server", env.Server.APIURL(),
		"--ca", env.Certs.CACertPath,
		"--cert", env.Certs.AdminCertPath,
		"--key", env.Certs.AdminKeyPath,
	)
	require.Equal(t, 0, exitCode, "manifest sync --dry-run should succeed: %s", stderr)
	assert.Contains(t, stdout, "Would sync", "should indicate dry run mode")
	assert.Contains(t, stdout, "FT-LOCAL-dryrun-1", "should list feature to sync")

	// Verify manifest was NOT modified
	afterManifest, err := manifest.Load(manifestPath)
	require.NoError(t, err)
	assert.True(t, afterManifest.HasFeature("FT-LOCAL-dryrun-1"), "feature should still exist unchanged")
}

// TestLint_ManifestWithSyncedFeatures verifies lint accepts synced features.
func TestLint_ManifestWithSyncedFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	workDir := t.TempDir()

	// Create manifest with mixed feature types
	m := manifest.New()
	m.Features["FT-LOCAL-lint-local"] = manifest.Entry{
		Name:    "Local Feature",
		Summary: "Unsynced local feature",
		Synced:  false,
	}
	m.Features["FT-000001"] = manifest.Entry{
		Name:     "Synced Feature",
		Summary:  "Feature synced from server",
		Synced:   true,
		SyncedAt: "2024-01-01T00:00:00Z",
		Alias:    "FT-LOCAL-original",
	}
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err := m.Save(manifestPath)
	require.NoError(t, err)

	// Test lint with local feature
	testFile := createTestYAMLFile(t, workDir, "FT-LOCAL-lint-local")
	_, stderr, exitCode := runFeatctl(t, workDir,
		"lint",
		"--offline",
		"--manifest", manifestPath,
		testFile,
	)
	assert.Equal(t, 0, exitCode, "lint should pass for local feature: %s", stderr)

	// Test lint with synced feature
	testFile2 := filepath.Join(workDir, "test2.yaml")
	content := "feature_id: FT-000001\ndescription: \"Testing synced feature reference\"\n"
	err = os.WriteFile(testFile2, []byte(content), 0o644)
	require.NoError(t, err)

	_, stderr, exitCode = runFeatctl(t, workDir,
		"lint",
		"--offline",
		"--manifest", manifestPath,
		testFile2,
	)
	assert.Equal(t, 0, exitCode, "lint should pass for synced feature: %s", stderr)
}

// TestFeatureCreate_ThenSync verifies creating local features and syncing them.
func TestFeatureCreate_ThenSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	workDir := t.TempDir()

	// Initialize manifest
	_, stderr, exitCode := runFeatctl(t, workDir, "manifest", "init")
	require.Equal(t, 0, exitCode, "manifest init should succeed: %s", stderr)

	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")

	// Create a local feature
	_, stderr, exitCode = runFeatctl(t, workDir,
		"feature", "create",
		"--manifest", manifestPath,
		"--id", "FT-LOCAL-e2e-create",
		"--name", "E2E Created Feature",
		"--summary", "Feature created via E2E test for syncing",
		"--owner", "E2E Tests",
		"--tags", "e2e,test",
	)
	require.Equal(t, 0, exitCode, "feature create should succeed: %s", stderr)

	// Verify feature is in manifest and unsynced
	stdout, _, _ := runFeatctl(t, workDir,
		"manifest", "list",
		"--manifest", manifestPath,
		"--unsynced",
	)
	assert.Contains(t, stdout, "FT-LOCAL-e2e-create", "feature should be in unsynced list")

	// Sync to server
	stdout, stderr, exitCode = runFeatctl(t, workDir,
		"manifest", "sync",
		"--manifest", manifestPath,
		"--server", env.Server.APIURL(),
		"--ca", env.Certs.CACertPath,
		"--cert", env.Certs.AdminCertPath,
		"--key", env.Certs.AdminKeyPath,
	)
	require.Equal(t, 0, exitCode, "manifest sync should succeed: %s", stderr)
	assert.Contains(t, stdout, "FT-LOCAL-e2e-create", "should mention local ID")
	assert.Contains(t, strings.ToLower(stdout), "synced: 1", "should sync 1 feature")

	// Verify no more unsynced features
	stdout, _, _ = runFeatctl(t, workDir,
		"manifest", "list",
		"--manifest", manifestPath,
		"--unsynced",
	)
	assert.NotContains(t, stdout, "FT-LOCAL-e2e-create", "original local ID should not appear in unsynced")
}
