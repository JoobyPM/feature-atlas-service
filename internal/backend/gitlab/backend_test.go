package gitlab

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/gitlab-org/api/client-go"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

func TestParseFeatureFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    *backend.Feature
		wantErr bool
	}{
		{
			name: "valid feature with all fields",
			content: `id: FT-000001
name: Authentication Flow
summary: End-to-end user authentication feature
owner: platform-team
tags: [backend, security]
created_at: "2026-01-22T10:00:00Z"
updated_at: "2026-01-22T11:00:00Z"
`,
			want: &backend.Feature{
				ID:        "FT-000001",
				Name:      "Authentication Flow",
				Summary:   "End-to-end user authentication feature",
				Owner:     "platform-team",
				Tags:      []string{"backend", "security"},
				CreatedAt: time.Date(2026, 1, 22, 10, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 1, 22, 11, 0, 0, 0, time.UTC),
			},
			wantErr: false,
		},
		{
			name: "minimal feature (required fields only)",
			content: `id: FT-000002
name: Simple Feature
`,
			want: &backend.Feature{
				ID:   "FT-000002",
				Name: "Simple Feature",
			},
			wantErr: false,
		},
		{
			name:    "missing id",
			content: `name: No ID Feature`,
			wantErr: true,
		},
		{
			name:    "missing name",
			content: `id: FT-000003`,
			wantErr: true,
		},
		{
			name:    "invalid yaml",
			content: `{{{not yaml`,
			wantErr: true,
		},
		{
			name: "invalid created_at format",
			content: `id: FT-000004
name: Bad Date
created_at: "not-a-date"
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFeatureFile([]byte(tt.content))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.ID, got.ID)
			assert.Equal(t, tt.want.Name, got.Name)
			assert.Equal(t, tt.want.Summary, got.Summary)
			assert.Equal(t, tt.want.Owner, got.Owner)
			assert.Equal(t, tt.want.Tags, got.Tags)
			if !tt.want.CreatedAt.IsZero() {
				assert.Equal(t, tt.want.CreatedAt, got.CreatedAt)
			}
			if !tt.want.UpdatedAt.IsZero() {
				assert.Equal(t, tt.want.UpdatedAt, got.UpdatedAt)
			}
		})
	}
}

func TestFormatFeatureFile(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	feature := &backend.Feature{
		ID:        "FT-000001",
		Name:      "Test Feature",
		Summary:   "A test feature",
		Owner:     "test-team",
		Tags:      []string{"test", "demo"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	content, err := FormatFeatureFile(feature)
	require.NoError(t, err)
	assert.NotEmpty(t, content)

	// Parse it back
	parsed, err := ParseFeatureFile(content)
	require.NoError(t, err)
	assert.Equal(t, feature.ID, parsed.ID)
	assert.Equal(t, feature.Name, parsed.Name)
	assert.Equal(t, feature.Summary, parsed.Summary)
	assert.Equal(t, feature.Owner, parsed.Owner)
	assert.Equal(t, feature.Tags, parsed.Tags)
}

func TestFeatureFilePath(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"FT-000001", "features/FT-000001.yaml"},
		{"FT-LOCAL-auth", "features/FT-LOCAL-auth.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := FeatureFilePath(tt.id)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFeatureIDFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"features/FT-000001.yaml", "FT-000001"},
		{"features/FT-LOCAL-auth.yaml", "FT-LOCAL-auth"},
		{"features/FT-123456.yaml", "FT-123456"},
		{"features/invalid.yaml", ""},
		{"features/FT-00001.yaml", ""},   // Wrong number of digits
		{"features/FT-0000001.yaml", ""}, // Too many digits
		{"other/FT-000001.yaml", "FT-000001"},
		{"FT-000001.yaml", "FT-000001"},
		{"features/readme.md", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := FeatureIDFromPath(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidFeatureID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"FT-000001", true},
		{"FT-123456", true},
		{"FT-LOCAL-auth", true},
		{"FT-LOCAL-something-long", true},
		{"FT-00001", false},   // Wrong number of digits
		{"FT-0000001", false}, // Too many digits
		{"ft-000001", false},  // Wrong case
		{"FT000001", false},   // Missing hyphen
		{"", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := IsValidFeatureID(tt.id)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseFeatureIDNumber(t *testing.T) {
	tests := []struct {
		id   string
		want int
	}{
		{"FT-000001", 1},
		{"FT-123456", 123456},
		{"FT-000000", 0},
		{"FT-LOCAL-auth", -1},
		{"invalid", -1},
		{"", -1},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := ParseFeatureIDNumber(tt.id)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNextFeatureID(t *testing.T) {
	tests := []struct {
		name        string
		existingIDs []string
		want        string
	}{
		{
			name:        "empty list",
			existingIDs: []string{},
			want:        "FT-000001",
		},
		{
			name:        "single ID",
			existingIDs: []string{"FT-000001"},
			want:        "FT-000002",
		},
		{
			name:        "multiple IDs",
			existingIDs: []string{"FT-000001", "FT-000005", "FT-000003"},
			want:        "FT-000006",
		},
		{
			name:        "with local IDs",
			existingIDs: []string{"FT-000001", "FT-LOCAL-auth", "FT-000002"},
			want:        "FT-000003",
		},
		{
			name:        "high numbers",
			existingIDs: []string{"FT-999998", "FT-000001"},
			want:        "FT-999999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextFeatureID(tt.existingIDs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterFeatures(t *testing.T) {
	features := []backend.Feature{
		{ID: "FT-000001", Name: "Authentication", Summary: "User auth flow"},
		{ID: "FT-000002", Name: "Payments", Summary: "Payment processing"},
		{ID: "FT-000003", Name: "Auth Tokens", Summary: "Token management"},
	}

	tests := []struct {
		name    string
		query   string
		limit   int
		wantLen int
		wantIDs []string
	}{
		{
			name:    "empty query returns all",
			query:   "",
			limit:   0,
			wantLen: 3,
		},
		{
			name:    "empty query with limit",
			query:   "",
			limit:   2,
			wantLen: 2,
		},
		{
			name:    "query matches ID",
			query:   "FT-000001",
			limit:   0,
			wantLen: 1,
			wantIDs: []string{"FT-000001"},
		},
		{
			name:    "query matches name",
			query:   "auth",
			limit:   0,
			wantLen: 2,
			wantIDs: []string{"FT-000001", "FT-000003"},
		},
		{
			name:    "query matches summary",
			query:   "processing",
			limit:   0,
			wantLen: 1,
			wantIDs: []string{"FT-000002"},
		},
		{
			name:    "no matches",
			query:   "nonexistent",
			limit:   0,
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterFeatures(features, tt.query, tt.limit)
			assert.Len(t, got, tt.wantLen)
			if tt.wantIDs != nil {
				gotIDs := make([]string, len(got))
				for i, f := range got {
					gotIDs[i] = f.ID
				}
				assert.ElementsMatch(t, tt.wantIDs, gotIDs)
			}
		})
	}
}

func TestSuggestFromFeatures(t *testing.T) {
	features := []backend.Feature{
		{ID: "FT-000001", Name: "Authentication", Summary: "User auth flow"},
		{ID: "FT-000002", Name: "API Gateway", Summary: "Gateway service"},
		{ID: "FT-000003", Name: "Auth Tokens", Summary: "Token management"},
	}

	tests := []struct {
		name        string
		query       string
		limit       int
		wantLen     int
		wantFirstID string
	}{
		{
			name:    "empty query returns all",
			query:   "",
			limit:   10,
			wantLen: 3,
		},
		{
			name:        "exact ID match first",
			query:       "FT-000001",
			limit:       10,
			wantLen:     1,
			wantFirstID: "FT-000001",
		},
		{
			name:        "ID prefix match",
			query:       "FT-00000",
			limit:       10,
			wantLen:     3,
			wantFirstID: "FT-000001", // First alphabetically among prefix matches
		},
		{
			name:        "name prefix matches multiple",
			query:       "auth",
			limit:       10,
			wantLen:     2,
			wantFirstID: "FT-000001", // Both match, sorted by ID
		},
		{
			name:    "limit respected",
			query:   "",
			limit:   1,
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuggestFromFeatures(features, tt.query, tt.limit)
			assert.Len(t, got, tt.wantLen)
			if tt.wantFirstID != "" && len(got) > 0 {
				assert.Equal(t, tt.wantFirstID, got[0].ID)
			}
		})
	}
}

func TestMatchScore(t *testing.T) {
	feature := backend.Feature{
		ID:      "FT-000001",
		Name:    "Authentication Flow",
		Summary: "User authentication feature",
	}

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"empty query", "", 1},
		{"exact ID", "ft-000001", 100},
		{"ID prefix", "ft-0000", 80},
		{"name prefix", "authentication", 60},
		{"ID contains", "00001", 40},
		{"name contains", "flow", 30},
		{"summary contains", "feature", 10},
		{"no match", "xyz", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchScore(feature, tt.query)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBackendMode(t *testing.T) {
	b := &Backend{}
	assert.Equal(t, backend.ModeGitLab, b.Mode())
}

func TestAccessLevelToRole(t *testing.T) {
	tests := []struct {
		name  string
		level gitlab.AccessLevelValue
		want  string
	}{
		{"owner", gitlab.OwnerPermissions, "owner"},
		{"maintainer", gitlab.MaintainerPermissions, "maintainer"},
		{"developer", gitlab.DeveloperPermissions, "developer"},
		{"reporter", gitlab.ReporterPermissions, "reporter"},
		{"guest", gitlab.GuestPermissions, "guest"},
		{"none", gitlab.NoPermissions, "none"},
		{"unknown", gitlab.AccessLevelValue(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := accessLevelToRole(tt.level)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMinInt(t *testing.T) {
	assert.Equal(t, 5, minInt(5, 10))
	assert.Equal(t, 5, minInt(10, 5))
	assert.Equal(t, 5, minInt(5, 5))
	assert.Equal(t, -5, minInt(-5, 0))
}
