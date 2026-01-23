package gitlab

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingMRsAddAndFind(t *testing.T) {
	p := &PendingMRs{
		Version: PendingMRsVersion,
		Pending: []PendingMR{},
	}

	mr := PendingMR{
		LocalID:   "FT-LOCAL-auth",
		ServerID:  "FT-000001",
		MRIID:     42,
		MRURL:     "https://gitlab.com/group/repo/-/merge_requests/42",
		Branch:    "feature/add-auth-x1y2",
		Operation: "create",
		CreatedAt: time.Now().UTC(),
	}

	p.Add(mr)

	assert.Equal(t, 1, p.Count())
	assert.False(t, p.IsEmpty())

	// Find by local ID
	found, ok := p.FindByLocalID("FT-LOCAL-auth")
	assert.True(t, ok)
	assert.Equal(t, mr.MRIID, found.MRIID)

	// Find by server ID
	found, ok = p.FindByServerID("FT-000001")
	assert.True(t, ok)
	assert.Equal(t, mr.LocalID, found.LocalID)

	// Find by MR IID
	found, ok = p.FindByMRIID(42)
	assert.True(t, ok)
	assert.Equal(t, mr.Branch, found.Branch)

	// Not found cases
	_, ok = p.FindByLocalID("FT-LOCAL-nonexistent")
	assert.False(t, ok)

	_, ok = p.FindByServerID("FT-999999")
	assert.False(t, ok)

	_, ok = p.FindByMRIID(999)
	assert.False(t, ok)
}

func TestPendingMRsRemove(t *testing.T) {
	p := &PendingMRs{
		Version: PendingMRsVersion,
		Pending: []PendingMR{
			{LocalID: "FT-LOCAL-auth", ServerID: "FT-000001", MRIID: 1},
			{LocalID: "FT-LOCAL-billing", ServerID: "FT-000002", MRIID: 2},
			{LocalID: "FT-LOCAL-payments", ServerID: "FT-000003", MRIID: 3},
		},
	}

	assert.Equal(t, 3, p.Count())

	// Remove by local ID
	p.Remove("FT-LOCAL-billing")
	assert.Equal(t, 2, p.Count())
	_, ok := p.FindByLocalID("FT-LOCAL-billing")
	assert.False(t, ok)

	// Remove by server ID
	p.RemoveByServerID("FT-000003")
	assert.Equal(t, 1, p.Count())

	// Remove non-existent - should not panic
	p.Remove("FT-LOCAL-nonexistent")
	assert.Equal(t, 1, p.Count())
}

func TestPendingMRsAddReplacesExisting(t *testing.T) {
	p := &PendingMRs{
		Version: PendingMRsVersion,
		Pending: []PendingMR{
			{LocalID: "FT-LOCAL-auth", MRIID: 1, MRURL: "old-url"},
		},
	}

	// Add same local ID with different MR
	p.Add(PendingMR{
		LocalID: "FT-LOCAL-auth",
		MRIID:   2,
		MRURL:   "new-url",
	})

	assert.Equal(t, 1, p.Count())
	found, _ := p.FindByLocalID("FT-LOCAL-auth")
	assert.Equal(t, 2, found.MRIID)
	assert.Equal(t, "new-url", found.MRURL)
}

func TestPendingMRsList(t *testing.T) {
	original := []PendingMR{
		{LocalID: "FT-LOCAL-auth"},
		{LocalID: "FT-LOCAL-billing"},
	}
	p := &PendingMRs{
		Version: PendingMRsVersion,
		Pending: original,
	}

	list := p.List()
	assert.Len(t, list, 2)

	// Verify it's a copy
	list[0].LocalID = "modified"
	assert.Equal(t, "FT-LOCAL-auth", p.Pending[0].LocalID)
}

func TestPendingMRsSaveAndLoad(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Change to temp directory for the test
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(oldWd)
	}()

	// Create a .git directory to simulate git root
	require.NoError(t, os.Mkdir(".git", 0o750))

	// Create and save pending MRs
	p := &PendingMRs{
		Version: PendingMRsVersion,
		Pending: []PendingMR{
			{
				LocalID:   "FT-LOCAL-test",
				ServerID:  "FT-000001",
				MRIID:     123,
				MRURL:     "https://gitlab.com/test/mr/123",
				Branch:    "feature/add-test-abcd",
				Operation: "create",
				CreatedAt: time.Date(2026, 1, 23, 10, 0, 0, 0, time.UTC),
			},
		},
	}

	err = p.Save()
	require.NoError(t, err)

	// Verify file exists
	path := filepath.Join(tmpDir, PendingMRsDir, PendingMRsFile)
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Load and verify
	loaded, err := LoadPendingMRs()
	require.NoError(t, err)

	assert.Equal(t, PendingMRsVersion, loaded.Version)
	assert.Len(t, loaded.Pending, 1)
	assert.Equal(t, "FT-LOCAL-test", loaded.Pending[0].LocalID)
	assert.Equal(t, 123, loaded.Pending[0].MRIID)
}

func TestLoadPendingMRsNoFile(t *testing.T) {
	// Create temp directory with no pending MRs file
	tmpDir := t.TempDir()

	oldWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(oldWd)
	}()

	// Create .git directory
	require.NoError(t, os.Mkdir(".git", 0o750))

	// Load should return empty PendingMRs
	loaded, err := LoadPendingMRs()
	require.NoError(t, err)

	assert.Equal(t, PendingMRsVersion, loaded.Version)
	assert.Empty(t, loaded.Pending)
	assert.True(t, loaded.IsEmpty())
}

func TestIsLocalID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"FT-LOCAL-auth", true},
		{"FT-LOCAL-billing-v2", true},
		{"FT-000001", false},
		{"FT-123456", false},
		{"", false},
		{"FT-LOCAL", false}, // Too short
		{"FT-LOCAL-", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := IsLocalID(tt.id)
			assert.Equal(t, tt.want, got)
		})
	}
}
