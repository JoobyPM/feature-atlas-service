//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JoobyPM/feature-atlas-service/internal/manifest"
	"github.com/JoobyPM/feature-atlas-service/test/integration/testutil"
)

// featctlPath returns the path to the featctl binary.
func featctlPath() string {
	// Get absolute path from environment if set (for CI)
	if p := os.Getenv("FEATCTL_PATH"); p != "" && fileExists(p) {
		return p
	}

	// Find from current working directory (assumes running from project root via make)
	if p := filepath.Join("bin", "featctl"); fileExists(p) {
		return p
	}

	// Find relative to test file location (when running tests directly)
	testDir, _ := os.Getwd()
	projectRoot := filepath.Join(testDir, "..", "..")
	if p := filepath.Join(projectRoot, "bin", "featctl"); fileExists(p) {
		absPath, _ := filepath.Abs(p)
		return absPath
	}

	// Fallback to PATH
	return "featctl"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runFeatctl executes featctl with the given arguments.
func runFeatctl(t *testing.T, workDir string, args ...string) (string, string, int) {
	t.Helper()

	cmd := exec.Command(featctlPath(), args...)
	cmd.Dir = workDir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run featctl: %v", err)
		}
	}

	return stdout.String(), stderr.String(), exitCode
}

// createTestYAMLFile creates a YAML file with feature reference for linting.
// The lint command expects a single feature_id and description at the root level.
func createTestYAMLFile(t *testing.T, dir, featureID string) string {
	t.Helper()

	content := "# Test configuration\nfeature_id: " + featureID + "\ndescription: \"This is a test description that is long enough to pass validation\"\n"

	path := filepath.Join(dir, "test-config.yaml")
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
	return path
}

// TestLint_ManifestFirst verifies that lint checks manifest before server.
func TestLint_ManifestFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create work directory
	workDir := t.TempDir()

	// Create manifest with a local feature
	m := manifest.New()
	localID := "FT-LOCAL-manifest-first"
	m.Features[localID] = manifest.Entry{
		Name:    "Manifest First Feature",
		Summary: "Should be found in manifest, not server",
		Synced:  false,
	}
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err = m.Save(manifestPath)
	require.NoError(t, err)

	// Create YAML file referencing the local feature
	testFile := createTestYAMLFile(t, workDir, localID)

	// Run lint with offline mode (manifest only)
	_, stderr, exitCode := runFeatctl(t, workDir,
		"lint",
		"--offline",
		"--manifest", manifestPath,
		testFile,
	)

	assert.Equal(t, 0, exitCode, "lint should pass for manifest feature: %s", stderr)
}

// TestLint_FallbackToServer verifies fallback when not in manifest.
func TestLint_FallbackToServer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create work directory
	workDir := t.TempDir()

	// Create empty manifest
	m := manifest.New()
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err = m.Save(manifestPath)
	require.NoError(t, err)

	// Get a valid server feature ID
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err)

	features, err := adminClient.Search(ctx, "", 1)
	require.NoError(t, err)
	require.NotEmpty(t, features)

	serverID := features[0].ID

	// Create YAML file referencing the server feature
	testFile := createTestYAMLFile(t, workDir, serverID)

	// Run lint (should fall back to server)
	_, stderr, exitCode := runFeatctl(t, workDir,
		"lint",
		"--manifest", manifestPath,
		"--server", env.Server.APIURL(),
		"--ca", env.Certs.CACertPath,
		"--cert", env.Certs.AdminCertPath,
		"--key", env.Certs.AdminKeyPath,
		testFile,
	)

	assert.Equal(t, 0, exitCode, "lint should pass for server feature: %s", stderr)
}

// TestLint_Offline_Valid verifies offline mode with valid manifest feature.
func TestLint_Offline_Valid(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create work directory
	workDir := t.TempDir()

	// Create manifest with local features
	m := manifest.New()
	m.Features["FT-LOCAL-offline-1"] = manifest.Entry{
		Name:    "Offline Feature 1",
		Summary: "For offline validation",
		Synced:  false,
	}
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err := m.Save(manifestPath)
	require.NoError(t, err)

	// Create YAML file referencing the feature
	testFile := createTestYAMLFile(t, workDir, "FT-LOCAL-offline-1")

	// Run lint in offline mode
	_, stderr, exitCode := runFeatctl(t, workDir,
		"lint",
		"--offline",
		"--manifest", manifestPath,
		testFile,
	)

	assert.Equal(t, 0, exitCode, "lint --offline should pass: %s", stderr)
}

// TestLint_Offline_NotFound verifies offline mode fails for missing feature.
func TestLint_Offline_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create work directory
	workDir := t.TempDir()

	// Create manifest WITHOUT the feature we'll reference
	m := manifest.New()
	m.Features["FT-LOCAL-exists"] = manifest.Entry{
		Name:    "Existing Feature",
		Summary: "This one exists",
		Synced:  false,
	}
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err := m.Save(manifestPath)
	require.NoError(t, err)

	// Create YAML file referencing a non-existent feature
	testFile := createTestYAMLFile(t, workDir, "FT-LOCAL-does-not-exist")

	// Run lint in offline mode
	_, stderr, exitCode := runFeatctl(t, workDir,
		"lint",
		"--offline",
		"--manifest", manifestPath,
		testFile,
	)

	assert.NotEqual(t, 0, exitCode, "lint --offline should fail for missing feature")
	assert.Contains(t, strings.ToLower(stderr), "not found", "error should mention feature not found")
}

// TestLint_ManifestPath verifies --manifest flag with custom path.
func TestLint_ManifestPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create work directory
	workDir := t.TempDir()

	// Create manifest in a custom location
	customDir := filepath.Join(workDir, "custom")
	err := os.MkdirAll(customDir, 0o755)
	require.NoError(t, err)

	m := manifest.New()
	m.Features["FT-LOCAL-custom-path"] = manifest.Entry{
		Name:    "Custom Path Feature",
		Summary: "Testing custom manifest path",
		Synced:  false,
	}
	customManifest := filepath.Join(customDir, "my-manifest.yaml")
	err = m.Save(customManifest)
	require.NoError(t, err)

	// Create YAML file in work directory
	testFile := createTestYAMLFile(t, workDir, "FT-LOCAL-custom-path")

	// Run lint with custom manifest path
	_, stderr, exitCode := runFeatctl(t, workDir,
		"lint",
		"--offline",
		"--manifest", customManifest,
		testFile,
	)

	assert.Equal(t, 0, exitCode, "lint should find feature in custom manifest: %s", stderr)
}

// TestLint_NoManifest_ServerOnly verifies fallback to server when no manifest exists.
func TestLint_NoManifest_ServerOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// Create work directory WITHOUT manifest
	workDir := t.TempDir()

	// Get a valid server feature ID
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err)

	features, err := adminClient.Search(ctx, "", 1)
	require.NoError(t, err)
	require.NotEmpty(t, features)

	serverID := features[0].ID

	// Create YAML file referencing the server feature
	testFile := createTestYAMLFile(t, workDir, serverID)

	// Run lint (no manifest, should use server only)
	_, stderr, exitCode := runFeatctl(t, workDir,
		"lint",
		"--server", env.Server.APIURL(),
		"--ca", env.Certs.CACertPath,
		"--cert", env.Certs.AdminCertPath,
		"--key", env.Certs.AdminKeyPath,
		testFile,
	)

	assert.Equal(t, 0, exitCode, "lint should pass using server: %s", stderr)
}

// TestLint_LocalFeature verifies lint accepts FT-LOCAL-* IDs from manifest.
func TestLint_LocalFeature(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create work directory
	workDir := t.TempDir()

	// Create manifest with various local feature formats
	m := manifest.New()
	localIDs := []string{
		"FT-LOCAL-a",
		"FT-LOCAL-feature-with-dashes",
		"FT-LOCAL-feature123",
	}
	for _, id := range localIDs {
		m.Features[id] = manifest.Entry{
			Name:    "Local Feature " + id,
			Summary: "Testing local ID format",
			Synced:  false,
		}
	}
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err := m.Save(manifestPath)
	require.NoError(t, err)

	// Test each local ID format individually
	for _, localID := range localIDs {
		testFile := createTestYAMLFile(t, workDir, localID)

		_, stderr, exitCode := runFeatctl(t, workDir,
			"lint",
			"--offline",
			"--manifest", manifestPath,
			testFile,
		)

		assert.Equal(t, 0, exitCode, "lint should accept %s: %s", localID, stderr)
	}
}

// TestLint_MixedFeatures verifies lint works with both local and synced features.
func TestLint_MixedFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create work directory
	workDir := t.TempDir()

	// Create manifest with mixed feature types
	m := manifest.New()
	m.Features["FT-LOCAL-unsynced"] = manifest.Entry{
		Name:    "Unsynced Feature",
		Summary: "Local only",
		Synced:  false,
	}
	m.Features["FT-000123"] = manifest.Entry{
		Name:     "Synced Feature",
		Summary:  "From server",
		Synced:   true,
		SyncedAt: "2024-01-01T00:00:00Z",
	}
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err := m.Save(manifestPath)
	require.NoError(t, err)

	// Test both feature types individually
	testIDs := []string{"FT-LOCAL-unsynced", "FT-000123"}
	for _, id := range testIDs {
		testFile := createTestYAMLFile(t, workDir, id)

		_, stderr, exitCode := runFeatctl(t, workDir,
			"lint",
			"--offline",
			"--manifest", manifestPath,
			testFile,
		)

		assert.Equal(t, 0, exitCode, "lint should handle %s: %s", id, stderr)
	}
}

// TestLint_InvalidFeatureID verifies lint rejects invalid feature ID format.
func TestLint_InvalidFeatureID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create work directory
	workDir := t.TempDir()

	// Create manifest
	m := manifest.New()
	manifestPath := filepath.Join(workDir, ".feature-atlas.yaml")
	err := m.Save(manifestPath)
	require.NoError(t, err)

	// Create YAML file with invalid feature ID
	testFile := createTestYAMLFile(t, workDir, "INVALID-ID-FORMAT")

	// Run lint
	_, stderr, exitCode := runFeatctl(t, workDir,
		"lint",
		"--offline",
		"--manifest", manifestPath,
		testFile,
	)

	assert.NotEqual(t, 0, exitCode, "lint should reject invalid feature ID format")
	// The error message should indicate the ID is invalid or not found
	assert.True(t,
		strings.Contains(strings.ToLower(stderr), "invalid") ||
			strings.Contains(strings.ToLower(stderr), "not found"),
		"error should indicate invalid ID: %s", stderr)
}
