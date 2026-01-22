// Package cache provides local feature caching for validation hints.
// This is NOT a source of truth - it's a performance optimization for the TUI.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JoobyPM/feature-atlas-service/internal/manifest"
)

// Directory and file names.
const (
	DirName      = ".fas"
	FeaturesFile = "features.json"
	MetaFile     = "meta.json"
	DefaultTTL   = 1 * time.Hour
)

// CachedFeature stores minimal feature data for validation.
type CachedFeature struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// CachedFeatures is the features.json structure.
type CachedFeatures struct {
	Version  string          `json:"version"`
	Features []CachedFeature `json:"features"`
}

// Meta is the meta.json structure.
type Meta struct {
	Version      string    `json:"version"`
	LastSync     time.Time `json:"last_sync"`
	ServerURL    string    `json:"server_url"`
	TTLSeconds   int       `json:"ttl_seconds"`
	FeatureCount int       `json:"feature_count"`
	IsComplete   bool      `json:"is_complete"`
}

// Cache provides local feature caching for validation hints.
type Cache struct {
	dir  string
	mu   sync.RWMutex
	data *CachedFeatures
	meta *Meta
}

// ResolveDir determines the cache directory location.
// Order: git root → manifest directory → CWD.
func ResolveDir() (string, error) {
	// 1. Try git root
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return filepath.Join(strings.TrimSpace(string(out)), DirName), nil
	}

	// 2. Try manifest directory
	manifestPath, err := manifest.Discover("")
	if err == nil {
		return filepath.Join(filepath.Dir(manifestPath), DirName), nil
	}

	// 3. Fall back to CWD
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}
	return filepath.Join(cwd, DirName), nil
}

// New creates a cache instance. Does not load data yet.
func New(dir string) *Cache {
	return &Cache{dir: dir}
}

// Dir returns the cache directory path.
func (c *Cache) Dir() string {
	return c.dir
}

// Load reads cache from disk.
// Returns nil if cache doesn't exist (normal case).
// Returns error if cache exists but is corrupt (caller should delete and recreate).
func (c *Cache) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Load features
	featPath := filepath.Join(c.dir, FeaturesFile)
	featData, featErr := os.ReadFile(featPath) //nolint:gosec // Cache path from ResolveDir
	if featErr == nil {
		var cf CachedFeatures
		if unmarshalErr := json.Unmarshal(featData, &cf); unmarshalErr != nil {
			return fmt.Errorf("corrupt features cache: %w", unmarshalErr)
		}
		c.data = &cf
	} else if !os.IsNotExist(featErr) {
		return fmt.Errorf("read features cache: %w", featErr)
	}

	// Load meta
	metaPath := filepath.Join(c.dir, MetaFile)
	metaData, metaErr := os.ReadFile(metaPath) //nolint:gosec // Cache path from ResolveDir
	if metaErr == nil {
		var cm Meta
		if unmarshalErr := json.Unmarshal(metaData, &cm); unmarshalErr != nil {
			return fmt.Errorf("corrupt meta cache: %w", unmarshalErr)
		}
		c.meta = &cm
	} else if !os.IsNotExist(metaErr) {
		return fmt.Errorf("read meta cache: %w", metaErr)
	}

	return nil
}

// Save writes cache to disk. Thread-safe.
// Uses write lock to ensure exclusive access during disk write.
func (c *Cache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	//nolint:gosec // Cache dir from ResolveDir, 0755 allows other tools to read
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	if c.data != nil {
		data, err := json.MarshalIndent(c.data, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal features: %w", err)
		}
		//nolint:gosec // Cache file, not sensitive, 0644 allows read by other tools
		if err := os.WriteFile(filepath.Join(c.dir, FeaturesFile), data, 0o644); err != nil {
			return fmt.Errorf("write features: %w", err)
		}
	}

	if c.meta != nil {
		data, err := json.MarshalIndent(c.meta, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal meta: %w", err)
		}
		//nolint:gosec // Cache file, not sensitive, 0644 allows read by other tools
		if err := os.WriteFile(filepath.Join(c.dir, MetaFile), data, 0o644); err != nil {
			return fmt.Errorf("write meta: %w", err)
		}
	}

	return nil
}

// IsStale returns true if cache is older than TTL.
func (c *Cache) IsStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.meta == nil {
		return true
	}
	ttl := time.Duration(c.meta.TTLSeconds) * time.Second
	if ttl == 0 {
		ttl = DefaultTTL
	}
	return time.Since(c.meta.LastSync) > ttl
}

// IsComplete returns true if cache contains all server features.
func (c *Cache) IsComplete() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.meta != nil && c.meta.IsComplete
}

// HasData returns true if cache has any feature data loaded.
func (c *Cache) HasData() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.data != nil && len(c.data.Features) > 0
}

// FindByNameExact returns feature with exact case-insensitive name match.
// The returned pointer references data within the cache's internal slice.
// Do not retain the pointer across cache operations (Update/Add) as the
// underlying slice may be replaced. Extract needed values immediately.
func (c *Cache) FindByNameExact(name string) *CachedFeature {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.data == nil {
		return nil
	}

	for i := range c.data.Features {
		if strings.EqualFold(c.data.Features[i].Name, name) {
			return &c.data.Features[i]
		}
	}
	return nil
}

// Update replaces cache data. Thread-safe.
func (c *Cache) Update(features []CachedFeature, serverURL string, isComplete bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = &CachedFeatures{
		Version:  "1",
		Features: features,
	}
	c.meta = &Meta{
		Version:      "1",
		LastSync:     time.Now(),
		ServerURL:    serverURL,
		TTLSeconds:   int(DefaultTTL.Seconds()),
		FeatureCount: len(features),
		IsComplete:   isComplete,
	}
}

// Add appends a single feature to cache. Thread-safe.
func (c *Cache) Add(feature CachedFeature) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.data == nil {
		c.data = &CachedFeatures{Version: "1"}
	}
	c.data.Features = append(c.data.Features, feature)
	if c.meta != nil {
		c.meta.FeatureCount++
	}
}

// FeatureCount returns the number of cached features.
func (c *Cache) FeatureCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.data == nil {
		return 0
	}
	return len(c.data.Features)
}
