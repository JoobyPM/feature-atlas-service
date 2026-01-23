package gitlab

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"Feature_Name", "feature-name"},
		{"  spaces  ", "spaces"},
		{"Special!@#$%Chars", "special-chars"},
		{"MixedCASE", "mixedcase"},
		{"", ""},
		{"a-b-c", "a-b-c"},
		{"A Very Long Feature Name That Exceeds Thirty Characters Limit", "a-very-long-feature-name-that"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := slugify(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRandomSuffix(t *testing.T) {
	// Should return 4-character hex string
	suffix := randomSuffix()
	assert.Len(t, suffix, 4)

	// Should be different each time (with high probability)
	suffix2 := randomSuffix()
	assert.NotEqual(t, suffix, suffix2)
}

func TestBranchName(t *testing.T) {
	feature := &backend.Feature{
		ID:   "FT-000001",
		Name: "Authentication Flow",
	}

	tests := []struct {
		op      string
		wantPfx string
		wantSfx string
		hasRand bool
	}{
		{OpCreate, "feature/add-authentication-flow-", "", true},
		{OpUpdate, "feature/update-FT-000001-authentication-flow-", "", true},
		{OpDelete, "feature/delete-FT-000001-", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			got := BranchName(tt.op, feature)
			if tt.hasRand {
				assert.True(t, strings.HasPrefix(got, tt.wantPfx), "expected prefix %q, got %q", tt.wantPfx, got)
				// Check random suffix is 4 chars
				suffix := strings.TrimPrefix(got, tt.wantPfx)
				assert.Len(t, suffix, 4, "random suffix should be 4 chars")
			} else {
				assert.Equal(t, tt.wantPfx, got)
			}
		})
	}
}

func TestBranchNameEmptyName(t *testing.T) {
	feature := &backend.Feature{
		ID:   "FT-000001",
		Name: "",
	}

	branch := BranchName(OpCreate, feature)
	assert.True(t, strings.HasPrefix(branch, "feature/add-feature-"))
}

func TestCommitMessage(t *testing.T) {
	feature := &backend.Feature{
		ID:   "FT-000001",
		Name: "Authentication Flow",
	}

	tests := []struct {
		op   string
		want string
	}{
		{OpCreate, "feat: add feature Authentication Flow (ID: FT-000001)"},
		{OpUpdate, "chore: update feature Authentication Flow (ID: FT-000001)"},
		{OpDelete, "chore: delete feature Authentication Flow (ID: FT-000001)"},
	}

	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			got := CommitMessage(tt.op, feature)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMRTitle(t *testing.T) {
	feature := &backend.Feature{
		ID:   "FT-000001",
		Name: "Authentication Flow",
	}

	tests := []struct {
		op   string
		want string
	}{
		{OpCreate, "Add feature: Authentication Flow (ID: FT-000001)"},
		{OpUpdate, "Update feature: Authentication Flow (ID: FT-000001)"},
		{OpDelete, "Delete feature: Authentication Flow (ID: FT-000001)"},
	}

	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			got := MRTitle(tt.op, feature)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMRDescription(t *testing.T) {
	feature := &backend.Feature{
		ID:      "FT-000001",
		Name:    "Authentication Flow",
		Summary: "End-to-end user authentication",
		Owner:   "platform-team",
		Tags:    []string{"backend", "security"},
	}

	t.Run("create operation", func(t *testing.T) {
		desc := MRDescription(OpCreate, feature)
		assert.Contains(t, desc, "## Feature Proposal")
		assert.Contains(t, desc, "**Name:** Authentication Flow")
		assert.Contains(t, desc, "**ID:** FT-000001")
		assert.Contains(t, desc, "**Summary:** End-to-end user authentication")
		assert.Contains(t, desc, "**Owner:** platform-team")
		assert.Contains(t, desc, "**Tags:** backend, security")
		assert.Contains(t, desc, "YAML file added")
		assert.Contains(t, desc, "Check for duplicates")
	})

	t.Run("update operation", func(t *testing.T) {
		desc := MRDescription(OpUpdate, feature)
		assert.Contains(t, desc, "## Feature Update")
		assert.Contains(t, desc, "YAML file updated")
	})

	t.Run("delete operation", func(t *testing.T) {
		desc := MRDescription(OpDelete, feature)
		assert.Contains(t, desc, "## Feature Deletion")
		assert.Contains(t, desc, "YAML file removed")
		assert.Contains(t, desc, "Confirm deletion")
	})

	t.Run("minimal feature", func(t *testing.T) {
		minimal := &backend.Feature{
			ID:   "FT-000002",
			Name: "Minimal",
		}
		desc := MRDescription(OpCreate, minimal)
		assert.Contains(t, desc, "**Name:** Minimal")
		assert.Contains(t, desc, "**ID:** FT-000002")
		assert.NotContains(t, desc, "**Summary:**")
		assert.NotContains(t, desc, "**Owner:**")
		assert.NotContains(t, desc, "**Tags:**")
	})
}

func TestMRConfig(t *testing.T) {
	cfg := MRConfig{
		Labels:             []string{"feature", "review"},
		RemoveSourceBranch: true,
		DefaultAssignee:    "john.doe",
	}

	assert.Equal(t, []string{"feature", "review"}, cfg.Labels)
	assert.True(t, cfg.RemoveSourceBranch)
	assert.Equal(t, "john.doe", cfg.DefaultAssignee)
}
