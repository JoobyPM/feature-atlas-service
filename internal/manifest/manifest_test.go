package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()

	m := New()

	if m.Version != SchemaVersion {
		t.Errorf("Version = %q, want %q", m.Version, SchemaVersion)
	}
	if m.Features == nil {
		t.Error("Features should be initialized")
	}
	if len(m.Features) != 0 {
		t.Errorf("Features should be empty, got %d", len(m.Features))
	}
}

func TestValidateLocalID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		// Valid cases
		{"simple", "FT-LOCAL-auth", false},
		{"with-numbers", "FT-LOCAL-auth123", false},
		{"with-hyphens", "FT-LOCAL-auth-flow", false},
		{"complex", "FT-LOCAL-user-auth-v2-beta", false},
		{"single-char", "FT-LOCAL-a", false},
		{"max-length", "FT-LOCAL-abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz12", false},

		// Invalid cases
		{"empty", "", true},
		{"no-prefix", "auth-flow", true},
		{"wrong-prefix", "FT-auth-flow", true},
		{"uppercase-suffix", "FT-LOCAL-AUTH", true},
		{"leading-hyphen", "FT-LOCAL--auth", true},
		{"trailing-hyphen", "FT-LOCAL-auth-", true},
		{"special-chars", "FT-LOCAL-auth_flow", true},
		{"spaces", "FT-LOCAL-auth flow", true},
		{"too-long", "FT-LOCAL-abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz123", true},
		{"server-format", "FT-000123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateLocalID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateLocalID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestValidateServerID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id   string
		want bool
	}{
		{"FT-000000", true},
		{"FT-000123", true},
		{"FT-999999", true},
		{"FT-00000", false},   // 5 digits
		{"FT-0000000", false}, // 7 digits
		{"FT-LOCAL-x", false},
		{"FT-ABCDEF", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()
			got := ValidateServerID(tt.id)
			if got != tt.want {
				t.Errorf("ValidateServerID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestIsLocalID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id   string
		want bool
	}{
		{"FT-LOCAL-auth", true},
		{"FT-LOCAL-", true}, // Has prefix, even if invalid
		{"FT-000123", false},
		{"FT-local-auth", false}, // Case sensitive
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()
			got := IsLocalID(tt.id)
			if got != tt.want {
				t.Errorf("IsLocalID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestLoadSave(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFilename)

	// Create manifest
	m := New()
	m.Features["FT-LOCAL-test"] = Entry{
		Name:    "Test Feature",
		Summary: "A test feature",
		Owner:   "Test Team",
		Tags:    []string{"test", "unit"},
		Synced:  false,
	}
	m.Features["FT-000123"] = Entry{
		Name:     "Synced Feature",
		Summary:  "A synced feature",
		Synced:   true,
		SyncedAt: "2026-01-21T10:00:00Z",
		Alias:    "FT-LOCAL-old-name",
	}

	// Save
	if err := m.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Save() did not create file")
	}

	// Load
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify contents
	if loaded.Version != SchemaVersion {
		t.Errorf("Version = %q, want %q", loaded.Version, SchemaVersion)
	}
	if len(loaded.Features) != 2 {
		t.Errorf("Features count = %d, want 2", len(loaded.Features))
	}

	// Check local feature
	local, ok := loaded.Features["FT-LOCAL-test"]
	if !ok {
		t.Error("FT-LOCAL-test not found")
	} else {
		if local.Name != "Test Feature" {
			t.Errorf("Name = %q, want %q", local.Name, "Test Feature")
		}
		if local.Synced {
			t.Error("Synced should be false")
		}
	}

	// Check synced feature
	synced, ok := loaded.Features["FT-000123"]
	if !ok {
		t.Error("FT-000123 not found")
	} else {
		if !synced.Synced {
			t.Error("Synced should be true")
		}
		if synced.Alias != "FT-LOCAL-old-name" {
			t.Errorf("Alias = %q, want %q", synced.Alias, "FT-LOCAL-old-name")
		}
	}
}

func TestLoad_NotFound(t *testing.T) {
	t.Parallel()

	_, err := Load("/nonexistent/path/.feature-atlas.yaml")
	if !errors.Is(err, ErrManifestNotFound) {
		t.Errorf("Load() error = %v, want ErrManifestNotFound", err)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFilename)

	// Write invalid YAML
	if err := os.WriteFile(path, []byte("invalid: yaml: content: ["), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("Load() should fail on invalid YAML")
	}
}

func TestLoad_EmptyFeatures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFilename)

	// Write manifest with no features
	content := "version: \"1\"\nfeatures:\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if m.Features == nil {
		t.Error("Features should be initialized")
	}
}

func TestDiscover(t *testing.T) {
	// Not parallel because some subtests modify CWD (global state)

	t.Run("explicit path exists", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DefaultFilename)

		// Create file
		if err := os.WriteFile(path, []byte("version: \"1\""), 0o600); err != nil {
			t.Fatal(err)
		}

		found, err := Discover(path)
		if err != nil {
			t.Fatalf("Discover() error = %v", err)
		}
		if found != path {
			t.Errorf("Discover() = %q, want %q", found, path)
		}
	})

	t.Run("explicit path not found", func(t *testing.T) {
		_, err := Discover("/nonexistent/.feature-atlas.yaml")
		if err == nil {
			t.Error("Discover() should fail for nonexistent explicit path")
		}
	})

	// NOTE: Tests using os.Chdir cannot be parallel since CWD is global state.
	// They are run sequentially.

	t.Run("no manifest found", func(t *testing.T) {
		dir := t.TempDir()

		// Change to temp dir (no manifest)
		oldDir, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Skip("cannot chdir")
		}
		defer func() { _ = os.Chdir(oldDir) }()

		_, err := Discover("")
		if !errors.Is(err, ErrManifestNotFound) {
			t.Errorf("Discover() error = %v, want ErrManifestNotFound", err)
		}
	})

	t.Run("finds manifest in cwd", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DefaultFilename)

		// Create manifest in temp dir
		if err := os.WriteFile(path, []byte("version: \"1\""), 0o600); err != nil {
			t.Fatal(err)
		}

		// Change to temp dir
		oldDir, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Skip("cannot chdir")
		}
		defer func() { _ = os.Chdir(oldDir) }()

		found, err := Discover("")
		if err != nil {
			t.Fatalf("Discover() error = %v", err)
		}

		// Resolve symlinks for comparison (macOS: /var -> /private/var)
		wantResolved, _ := filepath.EvalSymlinks(path)
		foundResolved, _ := filepath.EvalSymlinks(found)
		if foundResolved != wantResolved {
			t.Errorf("Discover() = %q, want %q", found, path)
		}
	})

	t.Run("finds manifest in parent directory", func(t *testing.T) {
		parentDir := t.TempDir()
		childDir := filepath.Join(parentDir, "subdir")
		if err := os.Mkdir(childDir, 0o750); err != nil {
			t.Fatal(err)
		}
		manifestPath := filepath.Join(parentDir, DefaultFilename)

		// Create manifest in parent dir
		if err := os.WriteFile(manifestPath, []byte("version: \"1\""), 0o600); err != nil {
			t.Fatal(err)
		}

		// Change to child dir
		oldDir, _ := os.Getwd()
		if err := os.Chdir(childDir); err != nil {
			t.Skip("cannot chdir")
		}
		defer func() { _ = os.Chdir(oldDir) }()

		found, err := Discover("")
		if err != nil {
			t.Fatalf("Discover() error = %v", err)
		}

		// Resolve symlinks for comparison (macOS: /var -> /private/var)
		wantResolved, _ := filepath.EvalSymlinks(manifestPath)
		foundResolved, _ := filepath.EvalSymlinks(found)
		if foundResolved != wantResolved {
			t.Errorf("Discover() = %q, want %q", found, manifestPath)
		}
	})
}

func TestAddFeature(t *testing.T) {
	t.Parallel()

	t.Run("valid feature", func(t *testing.T) {
		t.Parallel()
		m := New()

		err := m.AddFeature("FT-LOCAL-auth", "Auth", "Authentication", "Team", []string{"auth"})
		if err != nil {
			t.Fatalf("AddFeature() error = %v", err)
		}

		entry, ok := m.Features["FT-LOCAL-auth"]
		if !ok {
			t.Fatal("Feature not added")
		}
		if entry.Name != "Auth" {
			t.Errorf("Name = %q, want %q", entry.Name, "Auth")
		}
		if entry.Synced {
			t.Error("Synced should be false")
		}
	})

	t.Run("invalid ID", func(t *testing.T) {
		t.Parallel()
		m := New()

		err := m.AddFeature("invalid-id", "Name", "Summary", "", nil)
		if err == nil {
			t.Error("AddFeature() should fail for invalid ID")
		}
	})

	t.Run("duplicate ID", func(t *testing.T) {
		t.Parallel()
		m := New()

		_ = m.AddFeature("FT-LOCAL-auth", "Auth", "Summary", "", nil)
		err := m.AddFeature("FT-LOCAL-auth", "Auth 2", "Summary 2", "", nil)
		if err == nil {
			t.Error("AddFeature() should fail for duplicate ID")
		}
	})

	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		m := New()

		err := m.AddFeature("FT-LOCAL-auth", "", "Summary", "", nil)
		if !errors.Is(err, ErrEmptyName) {
			t.Errorf("AddFeature() error = %v, want ErrEmptyName", err)
		}
	})

	t.Run("empty summary", func(t *testing.T) {
		t.Parallel()
		m := New()

		err := m.AddFeature("FT-LOCAL-auth", "Auth", "", "", nil)
		if !errors.Is(err, ErrEmptySummary) {
			t.Errorf("AddFeature() error = %v, want ErrEmptySummary", err)
		}
	})
}

func TestListFeatures(t *testing.T) {
	t.Parallel()

	m := New()
	m.Features["FT-LOCAL-a"] = Entry{Name: "A", Synced: false}
	m.Features["FT-LOCAL-b"] = Entry{Name: "B", Synced: false}
	m.Features["FT-000123"] = Entry{Name: "C", Synced: true}

	t.Run("all", func(t *testing.T) {
		t.Parallel()
		all := m.ListFeatures(false)
		if len(all) != 3 {
			t.Errorf("ListFeatures(false) count = %d, want 3", len(all))
		}
	})

	t.Run("unsynced only", func(t *testing.T) {
		t.Parallel()
		unsynced := m.ListFeatures(true)
		if len(unsynced) != 2 {
			t.Errorf("ListFeatures(true) count = %d, want 2", len(unsynced))
		}
		if _, ok := unsynced["FT-000123"]; ok {
			t.Error("Synced feature should not be in unsynced list")
		}
	})

	t.Run("returns copy not internal data", func(t *testing.T) {
		t.Parallel()
		m := New()
		m.Features["FT-LOCAL-test"] = Entry{Name: "Test", Synced: false}

		// Get list and modify it
		list := m.ListFeatures(false)
		delete(list, "FT-LOCAL-test")
		list["FT-LOCAL-new"] = Entry{Name: "New"}

		// Original should be unchanged
		if !m.HasFeature("FT-LOCAL-test") {
			t.Error("Original manifest should still have FT-LOCAL-test")
		}
		if m.HasFeature("FT-LOCAL-new") {
			t.Error("Original manifest should not have FT-LOCAL-new")
		}
	})
}

func TestHasFeature(t *testing.T) {
	t.Parallel()

	m := New()
	m.Features["FT-LOCAL-auth"] = Entry{Name: "Auth"}

	if !m.HasFeature("FT-LOCAL-auth") {
		t.Error("HasFeature() should return true for existing feature")
	}
	if m.HasFeature("FT-LOCAL-nonexistent") {
		t.Error("HasFeature() should return false for nonexistent feature")
	}
}

func TestGetFeature(t *testing.T) {
	t.Parallel()

	m := New()
	m.Features["FT-LOCAL-auth"] = Entry{
		Name:    "Auth",
		Summary: "Authentication flow",
		Owner:   "Security",
		Tags:    []string{"auth", "security"},
		Synced:  false,
	}

	t.Run("existing feature", func(t *testing.T) {
		t.Parallel()
		entry, ok := m.GetFeature("FT-LOCAL-auth")
		if !ok {
			t.Fatal("GetFeature() should return true for existing feature")
		}
		if entry.Name != "Auth" {
			t.Errorf("Name = %q, want %q", entry.Name, "Auth")
		}
		if entry.Summary != "Authentication flow" {
			t.Errorf("Summary = %q, want %q", entry.Summary, "Authentication flow")
		}
		if entry.Owner != "Security" {
			t.Errorf("Owner = %q, want %q", entry.Owner, "Security")
		}
		if len(entry.Tags) != 2 {
			t.Errorf("Tags length = %d, want 2", len(entry.Tags))
		}
	})

	t.Run("nonexistent feature", func(t *testing.T) {
		t.Parallel()
		_, ok := m.GetFeature("FT-LOCAL-nonexistent")
		if ok {
			t.Error("GetFeature() should return false for nonexistent feature")
		}
	})
}

func TestSaveWithLock_Concurrent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFilename)

	// Create initial manifest
	m := New()
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	// Run concurrent saves
	var wg sync.WaitGroup
	errCh := make(chan error, 10)

	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m := New()
			m.Features["FT-LOCAL-test"] = Entry{
				Name:    "Test",
				Summary: "Concurrent test",
			}
			if err := m.SaveWithLock(path); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// All saves should succeed (with locking)
	for err := range errCh {
		t.Errorf("SaveWithLock() error = %v", err)
	}

	// Verify final file is valid
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Version != SchemaVersion {
		t.Errorf("Version = %q, want %q", loaded.Version, SchemaVersion)
	}
}

func TestFilePermissions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, DefaultFilename)

	m := New()
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// Check permissions (0644)
	perm := info.Mode().Perm()
	if perm != 0o644 {
		t.Errorf("File permissions = %o, want 0644", perm)
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkLoad(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, DefaultFilename)

	// Create manifest with 100 features
	m := New()
	for i := range 100 {
		m.Features["FT-LOCAL-test-"+leftPad(i, 3)] = Entry{
			Name:    "Test Feature",
			Summary: "Test summary for benchmarking",
			Owner:   "Test Team",
			Tags:    []string{"test", "benchmark"},
			Synced:  i%2 == 0,
		}
	}
	if err := m.Save(path); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		_, _ = Load(path)
	}
}

func BenchmarkSave(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, DefaultFilename)

	// Create manifest with 100 features
	m := New()
	for i := range 100 {
		m.Features["FT-LOCAL-test-"+leftPad(i, 3)] = Entry{
			Name:    "Test Feature",
			Summary: "Test summary for benchmarking",
			Owner:   "Test Team",
			Tags:    []string{"test", "benchmark"},
			Synced:  i%2 == 0,
		}
	}

	b.ResetTimer()
	for range b.N {
		_ = m.Save(path)
	}
}

func BenchmarkSaveWithLock(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, DefaultFilename)

	// Create manifest with 100 features
	m := New()
	for i := range 100 {
		m.Features["FT-LOCAL-test-"+leftPad(i, 3)] = Entry{
			Name:    "Test Feature",
			Summary: "Test summary for benchmarking",
			Owner:   "Test Team",
			Tags:    []string{"test", "benchmark"},
			Synced:  i%2 == 0,
		}
	}
	// Create initial file for lock
	if err := m.Save(path); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		_ = m.SaveWithLock(path)
	}
}

func BenchmarkListFeatures(b *testing.B) {
	m := New()
	for i := range 100 {
		m.Features["FT-LOCAL-test-"+leftPad(i, 3)] = Entry{
			Name:   "Test",
			Synced: i%2 == 0,
		}
	}

	b.ResetTimer()
	for range b.N {
		_ = m.ListFeatures(false)
	}
}

func BenchmarkListFeaturesUnsyncedOnly(b *testing.B) {
	m := New()
	for i := range 100 {
		m.Features["FT-LOCAL-test-"+leftPad(i, 3)] = Entry{
			Name:   "Test",
			Synced: i%2 == 0,
		}
	}

	b.ResetTimer()
	for range b.N {
		_ = m.ListFeatures(true)
	}
}

func BenchmarkHasFeature(b *testing.B) {
	m := New()
	for i := range 100 {
		m.Features["FT-LOCAL-test-"+leftPad(i, 3)] = Entry{Name: "Test"}
	}

	b.ResetTimer()
	for range b.N {
		_ = m.HasFeature("FT-LOCAL-test-050")
	}
}

func BenchmarkAddFeature(b *testing.B) {
	b.ResetTimer()
	for range b.N {
		m := New()
		_ = m.AddFeature("FT-LOCAL-test", "Test", "Summary", "Owner", []string{"tag"})
	}
}

func BenchmarkValidateLocalID(b *testing.B) {
	for range b.N {
		_ = ValidateLocalID("FT-LOCAL-auth-flow-v2")
	}
}

func BenchmarkValidateServerID(b *testing.B) {
	for range b.N {
		_ = ValidateServerID("FT-000123")
	}
}

func BenchmarkDiscoverExplicit(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, DefaultFilename)

	m := New()
	if err := m.Save(path); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		_, _ = Discover(path)
	}
}

// leftPad is a helper for benchmarks
func leftPad(n, width int) string {
	s := ""
	for range width {
		s = "0" + s
	}
	ns := ""
	for n > 0 {
		ns = string(rune('0'+n%10)) + ns
		n /= 10
	}
	if ns == "" {
		ns = "0"
	}
	if len(ns) >= width {
		return ns
	}
	return s[:width-len(ns)] + ns
}
