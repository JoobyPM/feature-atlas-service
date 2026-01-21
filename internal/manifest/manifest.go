// Package manifest provides local feature catalog management.
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultFilename is the default manifest filename.
const DefaultFilename = ".feature-atlas.yaml"

// SchemaVersion is the current manifest schema version.
const SchemaVersion = "1"

// LockTimeout is the maximum time to wait for file lock.
const LockTimeout = 5 * time.Second

// ID format regexes.
var (
	serverIDRegex = regexp.MustCompile(`^FT-[0-9]{6}$`)
	localIDRegex  = regexp.MustCompile(`^FT-LOCAL-[a-z0-9-]{1,64}$`)
)

// Errors.
var (
	ErrManifestNotFound = errors.New("manifest not found")
	ErrInvalidID        = errors.New("invalid feature ID format")
	ErrIDExists         = errors.New("feature ID already exists in manifest")
	ErrLockTimeout      = errors.New("manifest locked by another process")
	ErrInvalidYAML      = errors.New("invalid YAML")
)

// Entry represents a feature in the manifest with sync metadata.
type Entry struct {
	Name     string   `yaml:"name"`
	Summary  string   `yaml:"summary"`
	Owner    string   `yaml:"owner,omitempty"`
	Tags     []string `yaml:"tags,omitempty"`
	Synced   bool     `yaml:"synced"`
	SyncedAt string   `yaml:"synced_at,omitempty"` // RFC3339 timestamp
	Alias    string   `yaml:"alias,omitempty"`     // Original local ID after sync
}

// Manifest represents the local feature catalog file.
type Manifest struct {
	Version  string           `yaml:"version"`
	Features map[string]Entry `yaml:"features"`
}

// New creates an empty manifest with the current schema version.
func New() *Manifest {
	return &Manifest{
		Version:  SchemaVersion,
		Features: make(map[string]Entry),
	}
}

// ValidateLocalID checks if an ID matches the local feature ID format.
// Format: FT-LOCAL-[a-z0-9-]{1,64} with no leading/trailing hyphens in suffix.
func ValidateLocalID(id string) error {
	if !localIDRegex.MatchString(id) {
		return fmt.Errorf("%w: must match FT-LOCAL-[a-z0-9-]{1,64}", ErrInvalidID)
	}

	// Check for leading/trailing hyphens in suffix
	suffix := strings.TrimPrefix(id, "FT-LOCAL-")
	if strings.HasPrefix(suffix, "-") || strings.HasSuffix(suffix, "-") {
		return fmt.Errorf("%w: suffix cannot have leading or trailing hyphens", ErrInvalidID)
	}

	return nil
}

// ValidateServerID checks if an ID matches the server feature ID format.
// Format: FT-NNNNNN (6 digits).
func ValidateServerID(id string) error {
	if !serverIDRegex.MatchString(id) {
		return fmt.Errorf("%w: must match FT-NNNNNN (6 digits)", ErrInvalidID)
	}
	return nil
}

// IsLocalID returns true if the ID is a local (unsynced) feature ID.
func IsLocalID(id string) bool {
	return strings.HasPrefix(id, "FT-LOCAL-")
}

// Discover finds the manifest file by walking up the directory tree.
// Order: explicit path > CWD > parent directories up to git root.
func Discover(explicit string) (string, error) {
	// 1. Explicit path takes precedence
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("%w: %s", ErrManifestNotFound, explicit)
			}
			return "", err
		}
		return explicit, nil
	}

	// 2. Walk up from CWD
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		path := filepath.Join(dir, DefaultFilename)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}

		// Check if we've reached git root (stop point)
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			// We're at git root but didn't find manifest
			break
		}

		// Move to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return "", ErrManifestNotFound
}

// Load reads and parses a manifest from the given path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Path from Discover or user flag
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrManifestNotFound
		}
		return nil, err
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidYAML, err)
	}

	// Initialize map if nil (empty features section)
	if m.Features == nil {
		m.Features = make(map[string]Entry)
	}

	return &m, nil
}

// Save writes the manifest to the given path without locking.
// Use SaveWithLock for concurrent-safe writes.
func (m *Manifest) Save(path string) error {
	return m.atomicWrite(path)
}

// SaveWithLock writes the manifest with exclusive file locking on the target.
// This prevents concurrent writes to the same manifest file.
// Uses atomic write (temp file + rename) to prevent corruption on crash.
func (m *Manifest) SaveWithLock(path string) error {
	// Open or create the target file for locking
	// Config file needs 0644 for read access by other tools
	lockFile, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644) //nolint:gosec // Config file, path from discovery
	if err != nil {
		return fmt.Errorf("open manifest for lock: %w", err)
	}
	defer lockFile.Close()

	// Acquire exclusive lock on target file
	if lockErr := acquireLock(lockFile, LockTimeout); lockErr != nil {
		return lockErr
	}
	defer releaseLock(lockFile)

	// Atomic write while holding lock
	return m.atomicWrite(path)
}

// atomicWrite performs a crash-safe write using temp file + rename.
func (m *Manifest) atomicWrite(path string) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	// Write to temp file in same directory (required for atomic rename)
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".feature-atlas-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Write data to temp file
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath) //nolint:errcheck // Best effort cleanup
		return fmt.Errorf("write manifest: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath) //nolint:errcheck // Best effort cleanup
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // Best effort cleanup
		return fmt.Errorf("close temp file: %w", err)
	}

	// Set permissions (0644 for config files, readable by all)
	if err := os.Chmod(tmpPath, 0o644); err != nil { //nolint:gosec // Config file needs read access
		_ = os.Remove(tmpPath) //nolint:errcheck // Best effort cleanup
		return fmt.Errorf("set permissions: %w", err)
	}

	// Atomic rename (overwrites target if exists)
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // Best effort cleanup
		return fmt.Errorf("rename manifest: %w", err)
	}

	return nil
}

// ErrEmptyName indicates the feature name is empty.
var ErrEmptyName = errors.New("feature name cannot be empty")

// ErrEmptySummary indicates the feature summary is empty.
var ErrEmptySummary = errors.New("feature summary cannot be empty")

// AddFeature adds a new local feature to the manifest.
func (m *Manifest) AddFeature(id, name, summary, owner string, tags []string) error {
	// Validate required fields
	if name == "" {
		return ErrEmptyName
	}
	if summary == "" {
		return ErrEmptySummary
	}

	// Validate ID format
	if err := ValidateLocalID(id); err != nil {
		return err
	}

	// Check for duplicate
	if _, exists := m.Features[id]; exists {
		return fmt.Errorf("%w: %s", ErrIDExists, id)
	}

	m.Features[id] = Entry{
		Name:    name,
		Summary: summary,
		Owner:   owner,
		Tags:    tags,
		Synced:  false,
	}

	return nil
}

// GetFeature retrieves a feature by ID.
func (m *Manifest) GetFeature(id string) (Entry, bool) {
	entry, ok := m.Features[id]
	return entry, ok
}

// HasFeature checks if a feature exists in the manifest.
func (m *Manifest) HasFeature(id string) bool {
	_, ok := m.Features[id]
	return ok
}

// ListFeatures returns a copy of all features, optionally filtered to unsynced only.
// The returned map is safe to modify without affecting the manifest.
func (m *Manifest) ListFeatures(unsyncedOnly bool) map[string]Entry {
	result := make(map[string]Entry, len(m.Features))
	for id, entry := range m.Features {
		if unsyncedOnly && entry.Synced {
			continue
		}
		result[id] = entry
	}
	return result
}
