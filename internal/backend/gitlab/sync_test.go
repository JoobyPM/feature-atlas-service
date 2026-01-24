package gitlab

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

func TestHasLocalChanges(t *testing.T) {
	tests := []struct {
		name   string
		local  LocalFeature
		remote backend.Feature
		want   bool
	}{
		{
			name: "no_changes",
			local: LocalFeature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    []string{"tag1", "tag2"},
			},
			remote: backend.Feature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    []string{"tag1", "tag2"},
			},
			want: false,
		},
		{
			name: "name_changed",
			local: LocalFeature{
				Name:    "New Name",
				Summary: "Test summary",
				Owner:   "test-owner",
			},
			remote: backend.Feature{
				Name:    "Old Name",
				Summary: "Test summary",
				Owner:   "test-owner",
			},
			want: true,
		},
		{
			name: "summary_changed",
			local: LocalFeature{
				Name:    "Test Feature",
				Summary: "New summary",
				Owner:   "test-owner",
			},
			remote: backend.Feature{
				Name:    "Test Feature",
				Summary: "Old summary",
				Owner:   "test-owner",
			},
			want: true,
		},
		{
			name: "owner_changed",
			local: LocalFeature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "new-owner",
			},
			remote: backend.Feature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "old-owner",
			},
			want: true,
		},
		{
			name: "tags_added",
			local: LocalFeature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    []string{"tag1", "tag2", "tag3"},
			},
			remote: backend.Feature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    []string{"tag1", "tag2"},
			},
			want: true,
		},
		{
			name: "tags_removed",
			local: LocalFeature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    []string{"tag1"},
			},
			remote: backend.Feature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    []string{"tag1", "tag2"},
			},
			want: true,
		},
		{
			name: "tags_different_order_same",
			local: LocalFeature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    []string{"tag2", "tag1"},
			},
			remote: backend.Feature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    []string{"tag1", "tag2"},
			},
			want: false, // Order doesn't matter
		},
		{
			name: "both_nil_tags",
			local: LocalFeature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    nil,
			},
			remote: backend.Feature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    nil,
			},
			want: false,
		},
		{
			name: "nil_vs_empty_tags",
			local: LocalFeature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    nil,
			},
			remote: backend.Feature{
				Name:    "Test Feature",
				Summary: "Test summary",
				Owner:   "test-owner",
				Tags:    []string{},
			},
			want: false, // Both empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasLocalChanges(tt.local, tt.remote)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTagsEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{"both_nil", nil, nil, true},
		{"both_empty", []string{}, []string{}, true},
		{"nil_vs_empty", nil, []string{}, true},
		{"same_order", []string{"a", "b"}, []string{"a", "b"}, true},
		{"different_order", []string{"b", "a"}, []string{"a", "b"}, true},
		{"different_length", []string{"a"}, []string{"a", "b"}, false},
		{"different_content", []string{"a", "c"}, []string{"a", "b"}, false},
		{"single_same", []string{"a"}, []string{"a"}, true},
		{"single_different", []string{"a"}, []string{"b"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tagsEqual(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasRemoteChanges(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-1 * time.Hour)
	future := now.Add(1 * time.Hour)

	tests := []struct {
		name   string
		local  LocalFeature
		remote backend.Feature
		want   bool
	}{
		{
			name: "no_synced_at",
			local: LocalFeature{
				SyncedAt: time.Time{}, // Zero value - never synced
			},
			remote: backend.Feature{
				UpdatedAt: now,
			},
			want: true, // Never-synced features should be considered as having remote changes
		},
		{
			name: "remote_updated_after_sync",
			local: LocalFeature{
				SyncedAt: past,
			},
			remote: backend.Feature{
				UpdatedAt: now,
			},
			want: true,
		},
		{
			name: "remote_updated_before_sync",
			local: LocalFeature{
				SyncedAt: future,
			},
			remote: backend.Feature{
				UpdatedAt: now,
			},
			want: false,
		},
		{
			name: "same_time",
			local: LocalFeature{
				SyncedAt: now,
			},
			remote: backend.Feature{
				UpdatedAt: now,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasRemoteChanges(tt.local, tt.remote)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLocalToFeature(t *testing.T) {
	local := LocalFeature{
		Name:    "Test Feature",
		Summary: "Test summary",
		Owner:   "test-owner",
		Tags:    []string{"tag1", "tag2"},
	}

	got := localToFeature("FT-LOCAL-test", local)

	assert.Equal(t, "FT-LOCAL-test", got.ID)
	assert.Equal(t, "Test Feature", got.Name)
	assert.Equal(t, "Test summary", got.Summary)
	assert.Equal(t, "test-owner", got.Owner)
	assert.Equal(t, []string{"tag1", "tag2"}, got.Tags)
}

func TestSyncActionTypes(t *testing.T) {
	// Verify action type constants are distinct
	types := []SyncActionType{
		ActionNone,
		ActionCreateMR,
		ActionPendingMR,
		ActionMRMerged,
		ActionUpdateLocal,
		ActionUpdateRemote,
		ActionConflict,
		ActionWarnNew,
	}

	seen := make(map[SyncActionType]bool)
	for _, typ := range types {
		assert.False(t, seen[typ], "duplicate action type: %d", typ)
		seen[typ] = true
	}
}

func TestMRStates(t *testing.T) {
	// Verify MR states match GitLab API values
	assert.Equal(t, MRState("opened"), MRStateOpen)
	assert.Equal(t, MRState("merged"), MRStateMerged)
	assert.Equal(t, MRState("closed"), MRStateClosed)
}

func TestSyncResult(t *testing.T) {
	result := &SyncResult{
		Actions: []SyncAction{
			{Type: ActionCreateMR, LocalID: "FT-LOCAL-test1"},
			{Type: ActionPendingMR, LocalID: "FT-LOCAL-test2"},
			{Type: ActionWarnNew, ServerID: "FT-000001"},
		},
	}

	assert.Len(t, result.Actions, 3)
}

func TestLocalFeature(t *testing.T) {
	local := LocalFeature{
		Name:      "Test",
		Summary:   "Summary",
		Owner:     "owner",
		Tags:      []string{"a", "b"},
		Synced:    true,
		SyncedAt:  time.Now(),
		UpdatedAt: time.Now(),
	}

	assert.Equal(t, "Test", local.Name)
	assert.True(t, local.Synced)
}
