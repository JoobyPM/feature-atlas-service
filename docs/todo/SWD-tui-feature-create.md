# TUI Feature Creation Form with Local Cache

## Goal

Add a user-friendly form to `featctl tui` for creating new features directly from the terminal. The form validates uniqueness against server (with local cache for performance), creates the feature on the server, and saves it locally in the manifest.

## Jira Reference

- Ticket: SWD-XXX (TBD)
- Related: SWD-tui-multi-select (extends existing TUI)

## Current Behavior

- `featctl tui` only supports searching and selecting existing features
- Feature creation requires separate `featctl manifest add` command
- No local cache exists for offline validation or performance optimization
- No way to create new server features from TUI

## Target Behavior

- Press `n` (new) in TUI to open feature creation form
- Form validates name uniqueness against server (cache accelerates repeated checks)
- On submit: creates feature on server → adds to local manifest
- Local cache (`.fas/`) stores server features for fast validation hints
- Form follows official Bubble Tea best practices using `huh` library

## Requirements

### R1: Feature Creation Form

**Form Fields:**

| Field | Type | Required | Validation |
|-------|------|----------|------------|
| Name | Text Input | Yes | 1-200 chars (matches server limit) |
| Summary | Text Area | Yes | 1-1000 chars (matches server limit) |
| Owner | Text Input | No | 0-100 chars (matches server limit) |
| Tags | Text Input | No | Comma-separated, max 10 tags |

**Form Layout (using `huh` library):**

```
┌─ Create New Feature ─────────────────────────────────────┐
│                                                          │
│  Name *                                                  │
│  ┌────────────────────────────────────────────────────┐ │
│  │ Authentication Service                             │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  Summary *                                               │
│  ┌────────────────────────────────────────────────────┐ │
│  │ Handles user authentication via OAuth2 and JWT     │ │
│  │ tokens with session management.                    │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  Owner (optional)                                        │
│  ┌────────────────────────────────────────────────────┐ │
│  │ platform-team                                      │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  Tags (comma-separated, optional)                        │
│  ┌────────────────────────────────────────────────────┐ │
│  │ auth, security, core                               │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ────────────────────────────────────────────────────── │
│  [Submit]  [Cancel]                                      │
│                                                          │
│  Tab: next field • Shift+Tab: prev • Enter: submit       │
└──────────────────────────────────────────────────────────┘
```

### R2: Name Uniqueness Validation

**Important**: The server does NOT enforce name uniqueness. Duplicate names are technically allowed but undesirable for UX. This validation is **client-side best-effort**.

**Validation Flow:**

```
┌─────────────────────────────────────────────────────────────┐
│                    User submits form                        │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  1. Check LOCAL CACHE (if fresh, < TTL)                     │
│     - Exact case-insensitive name match                     │
│     - If found: show warning (not blocking)                 │
│       "Similar feature exists: FT-000123 - Auth Service"    │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  2. Check SERVER (if cache incomplete or stale)             │
│     - Skip if cache is fresh AND complete                   │
│     - GET /api/v1/features?query=<name>&limit=50            │
│     - Client-side exact match filter (server uses Contains) │
│     - If exact match found: show warning, allow override    │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  3. Create feature (POST /admin/v1/features)                │
│     - If 403: "Admin role required"                         │
│     - If success: add to manifest, update cache             │
└─────────────────────────────────────────────────────────────┘
```

**Why warning, not blocking?**
- Server allows duplicates (no constraint)
- Different teams may intentionally have similar feature names
- Cache may be stale, leading to false positives
- User makes final decision

**Exact Match Logic (client-side):**

```go
// Server search uses Contains, we need exact match
func findExactNameMatch(results []Feature, name string) *Feature {
    for i := range results {
        if strings.EqualFold(results[i].Name, name) {
            return &results[i]
        }
    }
    return nil
}
```

### R3: Local Cache (`.fas/` directory)

**Purpose**: Performance optimization for validation hints. NOT a source of truth.

**Location Resolution (in order):**

1. Git repository root (if in a git repo)
2. Directory containing `.feature-atlas.yaml` (if manifest exists)
3. Current working directory (fallback)

```go
func resolveCacheDir() (string, error) {
    // 1. Try git root
    if gitRoot, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
        return filepath.Join(strings.TrimSpace(string(gitRoot)), ".fas"), nil
    }
    
    // 2. Try manifest directory
    if manifestPath, err := manifest.Discover(""); err == nil {
        return filepath.Join(filepath.Dir(manifestPath), ".fas"), nil
    }
    
    // 3. Fall back to CWD
    cwd, err := os.Getwd()
    if err != nil {
        return "", err
    }
    return filepath.Join(cwd, ".fas"), nil
}
```

**Directory Structure:**

```
.fas/
├── features.json    # Cached feature list
└── meta.json        # Cache metadata (TTL, last sync)
```

**Note**: Add `.fas/` to project's root `.gitignore`. No need for nested `.gitignore`.

**features.json Schema:**

```json
{
  "version": "1",
  "features": [
    {
      "id": "FT-000001",
      "name": "Authentication",
      "summary": "User authentication service"
    }
  ]
}
```

**meta.json Schema:**

```json
{
  "version": "1",
  "last_sync": "2026-01-21T12:00:00Z",
  "server_url": "https://feature-atlas.example.com",
  "ttl_seconds": 3600,
  "feature_count": 42,
  "is_complete": false
}
```

**`is_complete` field**: Indicates whether cache contains ALL server features. If `false`, always do server check for validation.

**Cache Behavior:**

| Scenario | Action |
|----------|--------|
| Cache fresh & complete | Use cache for hint, skip server pre-check |
| Cache fresh & incomplete | Use cache for hint, still check server |
| Cache stale (> TTL) | Refresh via message, use existing for hint |
| Cache missing | No hint, check server directly |
| Offline + cache exists | Use cache hint, warn user on submit |
| Offline + no cache | Allow creation, warn may be duplicate |

### R4: Creation Flow

**Success Flow:**

```
User fills form → Submit → Show "Checking..." →
  → Server name check (exact match filter) →
  → If duplicate warning: "Similar feature exists. Create anyway? [Y/N]" →
  → POST /admin/v1/features →
  → Server returns FT-XXXXXX →
  → Add to manifest →
  → Update cache (via message, not in command) →
  → Show success: "Created FT-000042 - Authentication"
```

**Error Handling:**

| Error | User Message |
|-------|--------------|
| Network error (pre-check) | "Cannot verify uniqueness. Create anyway? [Y/N]" |
| 403 Forbidden | "Admin role required to create features." |
| Network error (create) | "Server unreachable. Create as local feature instead? [Y/N]" |
| Server error | "Server error: [message]. Try again later." |

### R5: Keybindings Update

Add to existing TUI keybindings (in `StateSearching`):

| Key | Action |
|-----|--------|
| `n` | Open feature creation form |

Form-specific keybindings (in `StateCreating`):

| Key | Action |
|-----|--------|
| `Tab` | Next form field |
| `Shift+Tab` | Previous form field |
| `Enter` | Submit form (when valid) |
| `Esc` | Cancel form and return to search |
| `Ctrl+C` | Quit TUI entirely |

**Note**: `n` key in `StateConfirm` means "No" - no conflict since different states.

## Implementation

### Phase 1: Local Cache Package

**New file**: `internal/cache/cache.go`

```go
package cache

import (
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

// CacheMeta is the meta.json structure.
type CacheMeta struct {
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
    meta *CacheMeta
}

// ResolveDir determines the cache directory location.
// Order: git root → manifest directory → CWD
func ResolveDir() (string, error) {
    // 1. Try git root
    if out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output(); err == nil {
        return filepath.Join(strings.TrimSpace(string(out)), DirName), nil
    }
    
    // 2. Try manifest directory
    if manifestPath, err := manifest.Discover(""); err == nil {
        return filepath.Join(filepath.Dir(manifestPath), DirName), nil
    }
    
    // 3. Fall back to CWD
    cwd, err := os.Getwd()
    if err != nil {
        return "", err
    }
    return filepath.Join(cwd, DirName), nil
}

// New creates a cache instance. Does not load data yet.
func New(dir string) *Cache {
    return &Cache{dir: dir}
}

// Load reads cache from disk.
// Returns nil if cache doesn't exist (normal case).
// Returns error if cache exists but is corrupt (caller should delete and recreate).
func (c *Cache) Load() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Load features
    featPath := filepath.Join(c.dir, FeaturesFile)
    if data, err := os.ReadFile(featPath); err == nil {
        var cf CachedFeatures
        if err := json.Unmarshal(data, &cf); err != nil {
            return fmt.Errorf("corrupt features cache: %w", err)
        }
        c.data = &cf
    } else if !os.IsNotExist(err) {
        return fmt.Errorf("read features cache: %w", err)
    }
    
    // Load meta
    metaPath := filepath.Join(c.dir, MetaFile)
    if data, err := os.ReadFile(metaPath); err == nil {
        var cm CacheMeta
        if err := json.Unmarshal(data, &cm); err != nil {
            return fmt.Errorf("corrupt meta cache: %w", err)
        }
        c.meta = &cm
    } else if !os.IsNotExist(err) {
        return fmt.Errorf("read meta cache: %w", err)
    }
    
    return nil
}

// Save writes cache to disk. Thread-safe.
func (c *Cache) Save() error {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    if err := os.MkdirAll(c.dir, 0755); err != nil {
        return fmt.Errorf("create cache dir: %w", err)
    }
    
    if c.data != nil {
        data, err := json.MarshalIndent(c.data, "", "  ")
        if err != nil {
            return fmt.Errorf("marshal features: %w", err)
        }
        if err := os.WriteFile(filepath.Join(c.dir, FeaturesFile), data, 0644); err != nil {
            return fmt.Errorf("write features: %w", err)
        }
    }
    
    if c.meta != nil {
        data, err := json.MarshalIndent(c.meta, "", "  ")
        if err != nil {
            return fmt.Errorf("marshal meta: %w", err)
        }
        if err := os.WriteFile(filepath.Join(c.dir, MetaFile), data, 0644); err != nil {
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

// FindByNameExact returns feature with exact case-insensitive name match.
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
    c.meta = &CacheMeta{
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
```

### Phase 2: Form Component

**New file**: `internal/tui/form.go`

**Note**: This file uses style constants (`colorPrimary`, `colorGreen`, `helpStyle`, `itemDimStyle`, `errorStyle`) defined in `tui.go`. Since both files are in the same `tui` package, they're accessible.

```go
package tui

import (
    "context"
    "fmt"
    "strings"
    "time"

    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/huh"
    "github.com/charmbracelet/lipgloss"

    "github.com/JoobyPM/feature-atlas-service/internal/apiclient"
    "github.com/JoobyPM/feature-atlas-service/internal/cache"
)

// FormState tracks the form submission state.
type FormState int

const (
    FormStateEditing FormState = iota
    FormStateValidating
    FormStateConfirmDuplicate
    FormStateSubmitting
    FormStateSuccess
    FormStateError
)

// FormModel manages the feature creation form.
// IMPORTANT: Must be used as a pointer (*FormModel) because huh.Form stores
// pointers to the name/summary/owner/tags fields via Value(&m.field).
// Using value semantics would cause dangling pointers.
type FormModel struct {
    form   *huh.Form
    state  FormState
    
    // Form values (bound to huh fields via pointers - must be stable!)
    name    string
    summary string
    owner   string
    tags    string
    
    // Validation results
    duplicateFeature *apiclient.Feature
    
    // Result
    createdFeature *apiclient.Feature
    err            error
    done           bool  // True when user acknowledged success/error and wants to return
    
    // Dependencies
    client *apiclient.Client
    cache  *cache.Cache
}

// Form field validation limits (match server: handlers.go:183)
const (
    maxNameLen    = 200
    maxSummaryLen = 1000
    maxOwnerLen   = 100
    maxTags       = 10
)

// API query limits
const (
    duplicateCheckLimit = 50  // Limit for duplicate name search
    cacheRefreshLimit   = 100 // Limit for cache refresh (marks incomplete if hit)
)

// NewFormModel creates a new form model.
// Returns pointer to ensure huh.Form's Value() pointers remain valid.
func NewFormModel(client *apiclient.Client, c *cache.Cache) *FormModel {
    m := &FormModel{
        client: client,
        cache:  c,
        state:  FormStateEditing,
    }
    m.form = m.buildForm()
    return m
}

func (m *FormModel) buildForm() *huh.Form {
    return huh.NewForm(
        huh.NewGroup(
            huh.NewInput().
                Key("name").
                Title("Name").
                Description("Feature name (required)").
                Value(&m.name).
                Validate(func(s string) error {
                    s = strings.TrimSpace(s)
                    if s == "" {
                        return fmt.Errorf("name is required")
                    }
                    if len(s) > maxNameLen {
                        return fmt.Errorf("name too long (max %d)", maxNameLen)
                    }
                    return nil
                }),

            huh.NewText().
                Key("summary").
                Title("Summary").
                Description("Brief description (required)").
                Value(&m.summary).
                CharLimit(maxSummaryLen).
                Validate(func(s string) error {
                    s = strings.TrimSpace(s)
                    if s == "" {
                        return fmt.Errorf("summary is required")
                    }
                    if len(s) > maxSummaryLen {
                        return fmt.Errorf("summary too long (max %d)", maxSummaryLen)
                    }
                    return nil
                }),

            huh.NewInput().
                Key("owner").
                Title("Owner").
                Description("Team or person (optional)").
                Value(&m.owner).
                Validate(func(s string) error {
                    if len(s) > maxOwnerLen {
                        return fmt.Errorf("owner too long (max %d)", maxOwnerLen)
                    }
                    return nil
                }),

            huh.NewInput().
                Key("tags").
                Title("Tags").
                Description("Comma-separated (optional)").
                Value(&m.tags).
                Validate(func(s string) error {
                    if s == "" {
                        return nil
                    }
                    parts := strings.Split(s, ",")
                    if len(parts) > maxTags {
                        return fmt.Errorf("too many tags (max %d)", maxTags)
                    }
                    return nil
                }),
        ),
    ).WithTheme(huh.ThemeCharm())
}

// Init initializes the form.
func (m *FormModel) Init() tea.Cmd {
    return m.form.Init()
}

// Update handles messages for the form.
// Uses pointer receiver to maintain stable memory for huh.Form's Value() pointers.
func (m *FormModel) Update(msg tea.Msg) tea.Cmd {
    switch m.state {
    case FormStateEditing:
        return m.updateEditing(msg)
    case FormStateValidating:
        return m.updateValidating(msg)
    case FormStateConfirmDuplicate:
        return m.updateConfirmDuplicate(msg)
    case FormStateSubmitting:
        return m.updateSubmitting(msg)
    case FormStateSuccess:
        return m.updateSuccess(msg)
    case FormStateError:
        return m.updateError(msg)
    default:
        return nil
    }
}

func (m *FormModel) updateEditing(msg tea.Msg) tea.Cmd {
    // Handle form completion
    form, cmd := m.form.Update(msg)
    if f, ok := form.(*huh.Form); ok {
        m.form = f
    }
    
    if m.form.State == huh.StateCompleted {
        // Form submitted - start async validation
        m.state = FormStateValidating
        return m.checkDuplicateCmd()
    }
    
    return cmd
}

func (m *FormModel) updateValidating(msg tea.Msg) tea.Cmd {
    switch msg := msg.(type) {
    case duplicateCheckResultMsg:
        if msg.err != nil {
            // Network error - ask user if they want to proceed anyway
            m.err = msg.err
            m.state = FormStateConfirmDuplicate
            return nil
        }
        if msg.duplicate != nil {
            // Found duplicate - ask user to confirm
            m.duplicateFeature = msg.duplicate
            m.state = FormStateConfirmDuplicate
            return nil
        }
        // No duplicate - proceed to create
        m.state = FormStateSubmitting
        return m.createFeatureCmd()
    }
    return nil
}

func (m *FormModel) updateConfirmDuplicate(msg tea.Msg) tea.Cmd {
    if keyMsg, ok := msg.(tea.KeyMsg); ok {
        switch keyMsg.String() {
        case "y", "Y":
            // User confirmed - proceed to create
            m.state = FormStateSubmitting
            m.err = nil
            return m.createFeatureCmd()
        case "n", "N", "esc":
            // User cancelled - back to editing
            m.state = FormStateEditing
            m.duplicateFeature = nil
            m.err = nil
            m.form = m.buildForm() // Reset form
            return m.form.Init()
        }
    }
    return nil
}

func (m *FormModel) updateSubmitting(msg tea.Msg) tea.Cmd {
    switch msg := msg.(type) {
    case featureCreatedMsg:
        if msg.err != nil {
            m.err = msg.err
            m.state = FormStateError
            return nil
        }
        if msg.feature == nil {
            m.err = fmt.Errorf("server returned empty response")
            m.state = FormStateError
            return nil
        }
        m.createdFeature = msg.feature
        m.state = FormStateSuccess
        return nil
    }
    return nil
}

func (m *FormModel) updateSuccess(msg tea.Msg) tea.Cmd {
    // Any key press marks form as done (TUI will handle transition)
    if _, ok := msg.(tea.KeyMsg); ok {
        m.done = true
    }
    return nil
}

func (m *FormModel) updateError(msg tea.Msg) tea.Cmd {
    // Any key press returns to editing
    if _, ok := msg.(tea.KeyMsg); ok {
        m.state = FormStateEditing
        m.err = nil
        m.form = m.buildForm() // Reset form
        return m.form.Init()
    }
    return nil
}

// View renders the form.
func (m *FormModel) View() string {
    var b strings.Builder
    
    title := lipgloss.NewStyle().
        Bold(true).
        Foreground(lipgloss.Color(colorPrimary)).
        Render("Create New Feature")
    
    b.WriteString(title)
    b.WriteString("\n\n")
    
    switch m.state {
    case FormStateEditing:
        b.WriteString(m.form.View())
        b.WriteString("\n")
        b.WriteString(helpStyle.Render("Tab: next • Shift+Tab: prev • Enter: submit • Esc: cancel"))
        
    case FormStateValidating:
        b.WriteString(itemDimStyle.Render("Checking for duplicate names..."))
        
    case FormStateConfirmDuplicate:
        if m.err != nil {
            b.WriteString(errorStyle.Render(fmt.Sprintf("Warning: %v", m.err)))
            b.WriteString("\n\n")
            b.WriteString("Cannot verify uniqueness. Create anyway?\n\n")
        } else if m.duplicateFeature != nil {
            b.WriteString(errorStyle.Render("Similar feature already exists:"))
            b.WriteString("\n\n")
            b.WriteString(fmt.Sprintf("  %s - %s\n", m.duplicateFeature.ID, m.duplicateFeature.Name))
            b.WriteString("\n")
            b.WriteString("Create anyway?\n\n")
        }
        b.WriteString(helpStyle.Render("[Y]es  [N]o"))
        
    case FormStateSubmitting:
        b.WriteString(itemDimStyle.Render("Creating feature..."))
        
    case FormStateSuccess:
        b.WriteString(lipgloss.NewStyle().
            Foreground(lipgloss.Color(colorGreen)).
            Render(fmt.Sprintf("✓ Created %s - %s", m.createdFeature.ID, m.createdFeature.Name)))
        b.WriteString("\n\n")
        b.WriteString(helpStyle.Render("Press any key to continue"))
        
    case FormStateError:
        b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
        b.WriteString("\n\n")
        b.WriteString(helpStyle.Render("Press any key to go back"))
    }
    
    return b.String()
}

// Completed returns true if form is done (success).
func (m *FormModel) Completed() bool {
    return m.state == FormStateSuccess
}

// Cancelled returns true if user cancelled (huh handles Esc → StateAborted).
func (m *FormModel) Cancelled() bool {
    return m.form.State == huh.StateAborted
}

// Done returns true if user has acknowledged success and wants to return.
func (m *FormModel) Done() bool {
    return m.done
}

// GetCreatedFeature returns the created feature (if any).
func (m *FormModel) GetCreatedFeature() *apiclient.Feature {
    return m.createdFeature
}

// Messages for async operations
type duplicateCheckResultMsg struct {
    duplicate *apiclient.Feature
    err       error
}

type featureCreatedMsg struct {
    feature *apiclient.Feature
    err     error
}

// checkDuplicateCmd checks for duplicate feature name.
// Captures values needed for the async operation (safe to read from pointer).
func (m *FormModel) checkDuplicateCmd() tea.Cmd {
    // Capture values for closure (don't capture pointer to avoid races)
    name := strings.TrimSpace(m.name)
    client := m.client
    cacheRef := m.cache
    
    return func() tea.Msg {
        // 1. Quick cache check (only if cache is fresh and complete)
        if cacheRef != nil && !cacheRef.IsStale() && cacheRef.IsComplete() {
            if cached := cacheRef.FindByNameExact(name); cached != nil {
                // Cache is authoritative - return duplicate without server call
                return duplicateCheckResultMsg{
                    duplicate: &apiclient.Feature{
                        ID:   cached.ID,
                        Name: cached.Name,
                    },
                }
            }
            // Cache is complete and fresh, name not found = no duplicate
            return duplicateCheckResultMsg{}
        }
        
        // 2. Server check (authoritative when cache is stale/incomplete)
        if client != nil {
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            
            // Search for features with similar name
            results, err := client.Search(ctx, name, duplicateCheckLimit)
            if err != nil {
                return duplicateCheckResultMsg{err: err}
            }
            
            // Client-side exact match (server uses Contains)
            for i := range results {
                if strings.EqualFold(results[i].Name, name) {
                    return duplicateCheckResultMsg{duplicate: &results[i]}
                }
            }
            return duplicateCheckResultMsg{} // Server check passed, no duplicate
        }
        
        // 3. Offline and no usable cache - return error so user is warned
        return duplicateCheckResultMsg{
            err: fmt.Errorf("no server connection: cannot verify name uniqueness"),
        }
    }
}

// createFeatureCmd creates the feature on server.
// Captures values needed for the async operation.
func (m *FormModel) createFeatureCmd() tea.Cmd {
    // Capture values for closure
    client := m.client
    name := strings.TrimSpace(m.name)
    summary := strings.TrimSpace(m.summary)
    owner := strings.TrimSpace(m.owner)
    tagsStr := m.tags
    
    return func() tea.Msg {
        if client == nil {
            return featureCreatedMsg{err: fmt.Errorf("no server connection: cannot create feature")}
        }
        
        // Parse tags
        var tags []string
        if tagsStr != "" {
            parts := strings.Split(tagsStr, ",")
            tags = make([]string, 0, len(parts))
            for _, t := range parts {
                t = strings.TrimSpace(t)
                if t != "" {
                    tags = append(tags, t)
                }
            }
        }
        
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        
        req := apiclient.CreateFeatureRequest{
            Name:    name,
            Summary: summary,
            Owner:   owner,
            Tags:    tags,
        }
        
        feature, err := client.CreateFeature(ctx, req)
        if err != nil {
            return featureCreatedMsg{err: err}
        }
        
        return featureCreatedMsg{feature: feature}
    }
}
```

### Phase 3: TUI Integration

**Modify**: `internal/tui/tui.go`

**Add imports** (to existing import block):

```go
import (
    // ... existing imports ...
    
    "github.com/JoobyPM/feature-atlas-service/internal/cache"
    "github.com/JoobyPM/feature-atlas-service/internal/manifest"
)
```

**Add new state** (keep existing StateConfirm, not StateConfirming):

```go
// Add new state (keep existing StateConfirm, not StateConfirming)
const (
    StateSearching State = iota
    StateConfirm
    StateCreating    // NEW: Form view
    StateSelected
    StateQuitting
)

// Add to Model struct
type Model struct {
    // ... existing fields ...
    formModel    *FormModel   // Pointer: huh.Form stores pointers to FormModel fields
    cache        *cache.Cache
    manifest     *manifest.Manifest  // For adding created features
    cacheUpdated bool         // Tracks if we've already added created feature to cache
}

// Add to Options struct
type Options struct {
    // ... existing fields ...
    Cache    *cache.Cache
    Manifest *manifest.Manifest  // For adding created features
}

// Update New() to accept cache and manifest
func New(client *apiclient.Client, opts Options) Model {
    // ... existing code ...
    return Model{
        // ... existing fields ...
        cache:    opts.Cache,
        manifest: opts.Manifest,
    }
}

// In handleKeyMsg, handle 'n' key in StateSearching
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    // ... existing state checks ...
    
    // In StateSearching section, add:
    switch msg.String() {
    case "n":
        m.state = StateCreating
        m.formModel = NewFormModel(m.client, m.cache)
        return m, m.formModel.Init()
    // ... other cases ...
    }
}

// In Update(), handle StateCreating
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Handle StateCreating FIRST - forward ALL messages to form
    if m.state == StateCreating && m.formModel != nil {
        // Check for global quit (Ctrl+C)
        if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+c" {
            m.state = StateQuitting
            return m, tea.Quit
        }
        
        // Delegate ALL messages to form (KeyMsg, featureCreatedMsg, duplicateCheckResultMsg, etc.)
        cmd := m.formModel.Update(msg)
        
        // Check if user acknowledged success and wants to return
        if m.formModel.Done() {
            m.state = StateSearching
            m.formModel = nil
            m.cacheUpdated = false  // Reset for next form
            return m, nil
        }
        
        // Check if form completed successfully (first time only)
        // cacheUpdated flag prevents duplicate adds when cacheSavedMsg arrives
        if m.formModel.Completed() && !m.cacheUpdated {
            if created := m.formModel.GetCreatedFeature(); created != nil {
                var cmds []tea.Cmd
                if cmd != nil {
                    cmds = append(cmds, cmd)
                }
                
                // 1. Add to manifest (source of truth - async to avoid blocking)
                cmds = append(cmds, m.saveManifestCmd(manifest.Feature{
                    ID:       created.ID,
                    Name:     created.Name,
                    Summary:  created.Summary,
                    IsSynced: true,  // Created on server, so it's synced
                }))
                
                // 2. Add to cache (performance hint)
                if m.cache != nil {
                    m.cache.Add(cache.CachedFeature{
                        ID:      created.ID,
                        Name:    created.Name,
                        Summary: created.Summary,
                    })
                    cmds = append(cmds, m.saveCacheCmd())
                }
                
                m.cacheUpdated = true  // Mark as done
                return m, tea.Batch(cmds...)
            }
        }
        
        // Check if form was cancelled (huh handles Esc → StateAborted)
        if m.formModel.Cancelled() {
            m.state = StateSearching
            m.formModel = nil
            m.cacheUpdated = false  // Reset for next form
            return m, nil
        }
        
        return m, cmd
    }
    
    switch msg := msg.(type) {
    case tea.KeyMsg:
        return m.handleKeyMsg(msg)
    
    // Handle cache messages
    case cacheSavedMsg:
        // Cache saved (or failed - non-critical, don't block)
        // Could log msg.err for debugging if needed
        return m, nil
    
    // Handle manifest messages
    case manifestSavedMsg:
        // Manifest saved (or failed - feature already on server, this is secondary)
        // Could log msg.err for debugging if needed
        return m, nil
        
    // ... other message types ...
    }
}

// In View(), handle StateCreating
func (m Model) View() string {
    if m.state == StateCreating && m.formModel != nil {
        return m.formModel.View()
    }
    // ... existing view code ...
}

// Add cache save command (avoids race condition)
type cacheSavedMsg struct{ err error }

func (m Model) saveCacheCmd() tea.Cmd {
    cacheRef := m.cache  // Capture for closure
    return func() tea.Msg {
        if cacheRef == nil {
            return cacheSavedMsg{}
        }
        err := cacheRef.Save()
        return cacheSavedMsg{err: err}
    }
}

// Add manifest save command (async to avoid blocking TUI)
type manifestSavedMsg struct{ err error }

func (m Model) saveManifestCmd(feature manifest.Feature) tea.Cmd {
    manifestRef := m.manifest  // Capture for closure
    return func() tea.Msg {
        if manifestRef == nil {
            return manifestSavedMsg{}
        }
        if err := manifestRef.AddFeature(feature); err != nil {
            return manifestSavedMsg{err: fmt.Errorf("add feature: %w", err)}
        }
        if err := manifestRef.Save(); err != nil {
            return manifestSavedMsg{err: fmt.Errorf("save manifest: %w", err)}
        }
        return manifestSavedMsg{}
    }
}
```

### Phase 4: Cache Refresh (Race-Condition Safe)

**In tui.go** - refresh cache via messages, not direct mutation:

```go
// Messages for cache refresh
type cacheRefreshResultMsg struct {
    features  []cache.CachedFeature
    serverURL string
    complete  bool
    err       error
}

// Init triggers cache refresh if stale
func (m Model) Init() tea.Cmd {
    cmds := []tea.Cmd{
        textinput.Blink,
        m.fetchSuggestions(""),
    }
    
    // Check if cache needs refresh
    if m.cache != nil && m.cache.IsStale() {
        cmds = append(cmds, m.refreshCacheCmd())
    }
    
    return tea.Batch(cmds...)
}

// refreshCacheCmd fetches features and returns result via message
func (m Model) refreshCacheCmd() tea.Cmd {
    // Capture dependencies for closure (consistent pattern with other commands)
    clientRef := m.client
    serverURL := ""
    if clientRef != nil {
        serverURL = clientRef.BaseURL
    }
    
    return func() tea.Msg {
        if clientRef == nil {
            return cacheRefreshResultMsg{err: fmt.Errorf("no server connection: cannot refresh cache")}
        }
        
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        
        // Fetch features (API doesn't support pagination, so single request)
        features, err := clientRef.Search(ctx, "", cacheRefreshLimit)
        if err != nil {
            return cacheRefreshResultMsg{err: err}
        }
        
        allFeatures := make([]cache.CachedFeature, len(features))
        for i, f := range features {
            allFeatures[i] = cache.CachedFeature{
                ID:      f.ID,
                Name:    f.Name,
                Summary: f.Summary,
            }
        }
        
        // Mark incomplete if we hit limit (server may have more features)
        return cacheRefreshResultMsg{
            features:  allFeatures,
            serverURL: serverURL,
            complete:  len(features) < cacheRefreshLimit,
        }
    }
}

// In Update(), handle cache refresh result
case cacheRefreshResultMsg:
    if m.cache != nil && msg.err == nil {
        // Update cache data (in Update, not in Cmd - safe!)
        // Use msg.serverURL (captured at command creation, avoids nil dereference)
        m.cache.Update(msg.features, msg.serverURL, msg.complete)
        // Save to disk via command
        return m, m.saveCacheCmd()
    }
    return m, nil
```

## Acceptance Criteria

### Definition of Ready

- [x] Requirements are clear and understood
- [x] Dependencies identified (`huh` library)
- [x] Technical approach discussed/approved
- [x] Race conditions addressed
- [x] Test strategy determined

### Definition of Done

- [ ] Code implemented and reviewed
- [ ] All tests passing (unit, integration, e2e)
- [ ] Documentation updated (README, help text)
- [ ] No new linting errors
- [ ] Manual testing complete
- [ ] `.fas/` added to `.gitignore`

### Functional Criteria

1. [ ] `n` key opens creation form from search view
2. [ ] Form validates field lengths (match server limits)
3. [ ] Submit checks server for duplicate name (exact match)
4. [ ] Duplicate warning shows existing feature ID
5. [ ] User can proceed despite duplicate warning
6. [ ] Submit creates feature on server (returns FT-XXXXXX)
7. [ ] Created feature is automatically added to local manifest
8. [ ] Cache is updated via message (race-safe)
9. [ ] `Esc` cancels form and returns to search
10. [ ] Network errors show user-friendly message with option to proceed

### Non-Functional Criteria

1. [ ] Cache check < 50ms (local lookup)
2. [ ] Form renders correctly in terminals ≥ 80x24
3. [ ] No race conditions (verified with `-race` flag)
4. [ ] No data loss on network failure mid-creation

## Testing

### Unit Tests

| Test | File | Scenario |
|------|------|----------|
| `TestCache_Load` | `cache_test.go` | Load existing cache |
| `TestCache_Load_CorruptJSON` | `cache_test.go` | Corrupt JSON returns error |
| `TestCache_Save` | `cache_test.go` | Save features to cache |
| `TestCache_IsStale` | `cache_test.go` | TTL expiration check |
| `TestCache_FindByNameExact` | `cache_test.go` | Case-insensitive exact match |
| `TestCache_Update` | `cache_test.go` | Thread-safe update |
| `TestCache_Add` | `cache_test.go` | Thread-safe add |
| `TestCache_ResolveDir` | `cache_test.go` | Git root → manifest dir → CWD fallback |
| `TestFormModel_Validate` | `form_test.go` | Field validation (length limits) |
| `TestFormModel_DuplicateCheck` | `form_test.go` | Exact match filtering |
| `TestFormModel_States` | `form_test.go` | State transitions |
| `TestFormModel_Cancelled` | `form_test.go` | Cancelled() returns true when aborted |
| `TestFormModel_Done` | `form_test.go` | Done() returns true after user acknowledgment |

### Integration Tests

| Test | Scenario |
|------|----------|
| `TestCreateFeature_NoDuplicate` | Name available → creates successfully |
| `TestCreateFeature_WithDuplicate` | Name exists → warning → user confirms → creates |
| `TestCreateFeature_Forbidden` | Non-admin → 403 error |

### E2E Tests

| Test | Scenario |
|------|----------|
| `TestTUI_CreateFeature` | Form → submit → verify server + manifest |
| `TestTUI_CreateFeature_DuplicateWarning` | Duplicate warning shown, user can override |
| `TestTUI_CreateFeature_Cancel` | Esc returns to search view |

## Dependencies

### New Go Dependency

```bash
go get github.com/charmbracelet/huh
```

### Existing Dependencies (already in project)

- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/bubbles`
- `github.com/charmbracelet/lipgloss`

## Out of Scope

- Server-side name uniqueness constraint (would require API change)
- Edit existing features in TUI
- Delete features from TUI
- Bulk feature creation
- Cache synchronization across multiple machines
- Cache encryption for sensitive data
- Pagination in server API (would improve cache completeness)

## Technical Notes

### Why Warning Instead of Blocking Duplicates?

1. **Server allows duplicates** - No uniqueness constraint in database/store
2. **Cache may be stale** - Could falsely block valid names
3. **Search is fuzzy** - `Contains` match, not exact
4. **User autonomy** - Different teams may want similar names intentionally

### Why `huh` Library?

1. **Official Charm library** - Same maintainers as Bubble Tea
2. **Native Bubble Tea integration** - `huh.Form` implements `tea.Model`
3. **Built-in validation** - Synchronous validators for field-level checks
4. **Accessible mode** - Screen reader support
5. **Consistent styling** - Uses Lip Gloss under the hood

### Why `*FormModel` (Pointer)?

The `huh` library's `Value(&field)` method stores pointers to struct fields:

```go
huh.NewInput().Value(&m.name)  // Stores pointer to m.name
```

If `FormModel` were used by value:

```go
func NewFormModel() FormModel {
    m := FormModel{}
    m.form = huh.NewForm(...Value(&m.name)...)  // Pointer to local m.name
    return m  // m is COPIED, but form still has pointer to original (now invalid)
}
```

The returned copy has a form pointing to **dangling memory**. Using `*FormModel`:

```go
func NewFormModel() *FormModel {
    m := &FormModel{}  // Heap allocated, stable address
    m.form = huh.NewForm(...Value(&m.name)...)  // Pointer to m.name (valid)
    return m  // Returns pointer, no copy, form pointers remain valid
}
```

This ensures `huh.Form`'s internal pointers always point to valid memory.

### Async Validation Pattern

Since `huh` validators are synchronous, we handle async validation (server check) after form submission:

```
Form.Validate() [sync] → Form.Submit → checkDuplicateCmd() [async] → 
  → duplicateCheckResultMsg → FormStateConfirmDuplicate/FormStateSubmitting
```

### Race Condition Prevention

All cache mutations happen in `Update()`, never in `tea.Cmd`:

```go
// WRONG (race condition):
func (m Model) refreshCacheCmd() tea.Cmd {
    return func() tea.Msg {
        m.cache.Save(features)  // ← Mutation in Cmd!
    }
}

// CORRECT (race-safe):
func (m Model) refreshCacheCmd() tea.Cmd {
    return func() tea.Msg {
        return cacheRefreshResultMsg{features: features}
    }
}

// In Update():
case cacheRefreshResultMsg:
    m.cache.Update(msg.features, ...)  // ← Mutation in Update (single-threaded)
```

### Cache vs Manifest (SST Principle)

| Aspect | Manifest (`.feature-atlas.yaml`) | Cache (`.fas/`) |
|--------|----------------------------------|-----------------|
| Purpose | Source of truth for local features | Performance hint for validation |
| Persisted | Yes (committed to git) | No (gitignored) |
| Contains | User's selected features | All server features (best-effort) |
| Synced | Explicitly via `manifest sync` | Automatically on TUI start |
| Authoritative | Yes | No (always verify with server) |
