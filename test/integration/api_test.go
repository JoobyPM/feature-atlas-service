//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
	"github.com/JoobyPM/feature-atlas-service/test/integration/testutil"
)

// TestAdminCreateFeature verifies that an admin can create a feature.
func TestAdminCreateFeature(t *testing.T) {
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

	// Create a feature
	created, err := adminClient.CreateFeature(ctx, apiclient.CreateFeatureRequest{
		Name:    "Test Feature",
		Summary: "A feature created during integration testing",
		Owner:   "Test Team",
		Tags:    []string{"test", "integration"},
	})
	require.NoError(t, err, "create feature")

	// Verify the response
	assert.NotEmpty(t, created.ID, "feature ID should be assigned")
	assert.Regexp(t, `^FT-\d{6}$`, created.ID, "feature ID should match server format")
	assert.Equal(t, "Test Feature", created.Name)
	assert.Equal(t, "A feature created during integration testing", created.Summary)
	assert.Equal(t, "Test Team", created.Owner)
	assert.ElementsMatch(t, []string{"test", "integration"}, created.Tags)
	assert.False(t, created.CreatedAt.IsZero(), "created_at should be set")

	// Verify the feature can be retrieved
	retrieved, err := adminClient.GetFeature(ctx, created.ID)
	require.NoError(t, err, "get created feature")
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, created.Name, retrieved.Name)
}

// TestAdminCreateFeature_Unauthorized verifies that a non-admin cannot create features.
func TestAdminCreateFeature_Unauthorized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	// First, register the user client using admin credentials
	adminClient, err := testutil.NewAdminClient(env)
	require.NoError(t, err, "create admin client")

	err = testutil.RegisterUserClient(ctx, adminClient, env.Certs)
	require.NoError(t, err, "register user client")

	// Now create user client for testing
	userClient, err := testutil.NewUserClient(env)
	require.NoError(t, err, "create user client")

	// Attempt to create a feature as non-admin (user is registered but not admin)
	_, err = userClient.CreateFeature(ctx, apiclient.CreateFeatureRequest{
		Name:    "Unauthorized Feature",
		Summary: "This should fail",
	})
	require.Error(t, err, "non-admin should not be able to create features")
	// Server returns "admin only" for non-admin users trying to access /admin/ routes
	assert.Contains(t, err.Error(), "admin")
}

// TestAdminCreateFeature_BadRequest verifies validation errors.
func TestAdminCreateFeature_BadRequest(t *testing.T) {
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

	testCases := []struct {
		name    string
		req     apiclient.CreateFeatureRequest
		wantErr string
	}{
		{
			name:    "missing name",
			req:     apiclient.CreateFeatureRequest{Summary: "Has summary but no name"},
			wantErr: "name and summary required",
		},
		{
			name:    "missing summary",
			req:     apiclient.CreateFeatureRequest{Name: "Has name but no summary"},
			wantErr: "name and summary required",
		},
		{
			name:    "both missing",
			req:     apiclient.CreateFeatureRequest{},
			wantErr: "name and summary required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := adminClient.CreateFeature(ctx, tc.req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestGetFeature verifies fetching a seeded feature.
func TestGetFeature(t *testing.T) {
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

	// Search for seeded features to get a valid ID
	features, err := adminClient.Search(ctx, "", 1)
	require.NoError(t, err, "search features")
	require.NotEmpty(t, features, "should have at least one seeded feature")

	// Get the feature by ID
	feature, err := adminClient.GetFeature(ctx, features[0].ID)
	require.NoError(t, err, "get feature")
	assert.Equal(t, features[0].ID, feature.ID)
}

// TestGetFeature_NotFound verifies 404 for non-existent feature.
func TestGetFeature_NotFound(t *testing.T) {
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

	// Try to get a non-existent feature
	_, err = adminClient.GetFeature(ctx, "FT-999999")
	require.Error(t, err)
	assert.ErrorIs(t, err, apiclient.ErrFeatureNotFound)
}

// TestFeatureExists checks the existence helper.
func TestFeatureExists(t *testing.T) {
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

	// Get a valid feature ID
	features, err := adminClient.Search(ctx, "", 1)
	require.NoError(t, err, "search features")
	require.NotEmpty(t, features)

	// Check existence
	exists, err := adminClient.FeatureExists(ctx, features[0].ID)
	require.NoError(t, err)
	assert.True(t, exists, "seeded feature should exist")

	// Check non-existence
	exists, err = adminClient.FeatureExists(ctx, "FT-999999")
	require.NoError(t, err)
	assert.False(t, exists, "non-existent feature should not exist")
}

// TestSearch verifies feature search functionality.
func TestSearch(t *testing.T) {
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

	// Search with limit
	features, err := adminClient.Search(ctx, "", 5)
	require.NoError(t, err, "search features")
	assert.LessOrEqual(t, len(features), 5, "should respect limit")

	// All results should have required fields
	for _, f := range features {
		assert.NotEmpty(t, f.ID)
		assert.NotEmpty(t, f.Name)
		assert.NotEmpty(t, f.Summary)
	}
}

// TestSuggest verifies autocomplete functionality.
func TestSuggest(t *testing.T) {
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

	// Create a feature with a unique name
	created, err := adminClient.CreateFeature(ctx, apiclient.CreateFeatureRequest{
		Name:    "UniqueTestFeature123",
		Summary: "For suggestion testing",
	})
	require.NoError(t, err, "create feature")

	// Search for suggestions
	suggestions, err := adminClient.Suggest(ctx, "Unique", 10)
	require.NoError(t, err, "get suggestions")

	// Find our feature in suggestions
	var found bool
	for _, s := range suggestions {
		if s.ID == created.ID {
			found = true
			assert.Equal(t, "UniqueTestFeature123", s.Name)
			break
		}
	}
	assert.True(t, found, "created feature should appear in suggestions")
}

// TestMe verifies the /me endpoint.
func TestMe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	env, err := testutil.SetupTestEnv(ctx)
	require.NoError(t, err, "setup test environment")
	defer env.Cleanup(ctx)

	t.Run("admin client", func(t *testing.T) {
		adminClient, err := testutil.NewAdminClient(env)
		require.NoError(t, err)

		info, err := adminClient.Me(ctx)
		require.NoError(t, err)
		assert.Equal(t, "admin", info.Role)
		assert.NotEmpty(t, info.Fingerprint)
	})

	// Note: User client test requires registering the user cert first.
	// The server only bootstraps the admin client from file.
	// This tests the registration flow.
	t.Run("user client after registration", func(t *testing.T) {
		adminClient, err := testutil.NewAdminClient(env)
		require.NoError(t, err)

		// Register user certificate via admin API
		err = testutil.RegisterUserClient(ctx, adminClient, env.Certs)
		require.NoError(t, err, "register user client")

		// Now user should be able to authenticate
		userClient, err := testutil.NewUserClient(env)
		require.NoError(t, err)

		info, err := userClient.Me(ctx)
		require.NoError(t, err)
		assert.Equal(t, "user", info.Role)
		assert.NotEmpty(t, info.Fingerprint)
	})
}
