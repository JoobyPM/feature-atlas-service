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
		{"max-length", "FT-LOCAL-" + string(make([]byte, 64)), false}, // Will fail - need valid chars

		// Invalid cases
		{"empty", "", true},
		{"no-prefix", "auth-flow", true},
		{"wrong-prefix", "FT-auth-flow", true},
		{"uppercase-suffix", "FT-LOCAL-AUTH", true},
		{"leading-hyphen", "FT-LOCAL--auth", true},
		{"trailing-hyphen", "FT-LOCAL-auth-", true},
		{"special-chars", "FT-LOCAL-auth_flow", true},
		{"spaces", "FT-LOCAL-auth flow", true},
		{"too-long", "FT-LOCAL-" + string(make([]byte, 65)), true},
		{"server-format", "FT-000123", true},
	}

	// Fix max-length test case
	tests[5] = struct {
		name    string
		id      string
		wantErr bool
	}{"max-length", "FT-LOCAL-abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz12", false}

	// Fix too-long test case (65 chars after prefix)
	tests[12] = struct {
		name    string
		id      string
		wantErr bool
	}{"too-long", "FT-LOCAL-abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz123", true}

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
	t.Parallel()

	t.Run("explicit path exists", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
		_, err := Discover("/nonexistent/.feature-atlas.yaml")
		if err == nil {
			t.Error("Discover() should fail for nonexistent explicit path")
		}
	})

	t.Run("no manifest found", func(t *testing.T) {
		t.Parallel()
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
