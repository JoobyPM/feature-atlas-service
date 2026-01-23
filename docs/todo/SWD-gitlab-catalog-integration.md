# Feature: GitLab Catalog Integration

## Jira Reference
- **Ticket:** SWD-XXX *(to be assigned)*
- **Link:** *(to be added)*

## Overview

Extend `featctl` CLI to support dual-mode operation: existing Atlas backend (mTLS server) and new GitLab backend (Git-based catalog with MR workflow). Users can switch modes via configuration while using identical CLI/TUI commands.

## Status: 📋 PLANNING

---

## Definition of Ready (DoR)

- [x] Requirements documented and understood
- [x] Architecture decisions finalized (interface-first, DRY/SST compliant)
- [x] Dependencies identified (official GitLab SDK, go-keyring)
- [x] Deep research completed (authentication, MR workflow, sync strategy)
- [x] Technical approach approved (dual backend via `FeatureBackend` interface)
- [x] Acceptance criteria defined
- [x] Test strategy determined

## Definition of Done (DoD)

- [ ] Code implemented following DRY/SST principles
- [ ] All acceptance tests passing (unit, integration, e2e)
- [ ] `make fmt && make check` passes
- [ ] Documentation updated (README, CLI help)
- [ ] No new linting errors
- [ ] MR approved and merged

---

## Architecture

### Dual-Mode Design (DRY/SST Compliant)

```
┌──────────────────────────────────────────────────────────────┐
│                        CLI / TUI                             │
│              (uses FeatureBackend interface)                 │
└─────────────────────────┬────────────────────────────────────┘
                          │
          ┌───────────────┴───────────────┐
          │     internal/backend/         │
          │   FeatureBackend interface    │
          │   + common error types        │
          └───────────────┬───────────────┘
                          │
           ┌──────────────┴──────────────┐
           │                             │
┌──────────┴──────────┐       ┌──────────┴───────────┐
│   atlas/backend.go  │       │  gitlab/backend.go   │
│   (wraps apiclient) │       │  (gitlab-org SDK)    │
└─────────────────────┘       └──────────────────────┘
```

**Single Source of Truth:** Interface definition in `internal/backend/backend.go`
**DRY:** No duplicated logic in CLI/TUI — all operations go through interface

### Backend Interface

```go
// internal/backend/backend.go
package backend

import (
    "context"
    "errors"
    "time"
)

// Feature is the backend-agnostic feature type.
// NOTE: This is a NEW type, distinct from apiclient.Feature and manifest.Feature.
// Both backend implementations translate to/from this type.
type Feature struct {
    ID        string    // FT-NNNNNN (server) or FT-LOCAL-* (unsynced)
    Name      string
    Summary   string
    Owner     string
    Tags      []string
    CreatedAt time.Time // When feature was created
    UpdatedAt time.Time // When feature was last modified (for conflict detection)
}

// SuggestItem is the backend-agnostic autocomplete item.
type SuggestItem struct {
    ID      string
    Name    string
    Summary string
}

// AuthInfo provides authenticated user information.
type AuthInfo struct {
    Username    string // Certificate CN (Atlas) or GitLab username
    DisplayName string // Human-readable name
    Role        string // "admin"/"user" (Atlas) or "owner"/"maintainer"/"developer" (GitLab)
}

// FeatureBackend is the interface both Atlas and GitLab backends implement.
type FeatureBackend interface {
    // Read operations
    Suggest(ctx context.Context, query string, limit int) ([]SuggestItem, error)
    Search(ctx context.Context, query string, limit int) ([]Feature, error)
    GetFeature(ctx context.Context, id string) (*Feature, error)
    FeatureExists(ctx context.Context, id string) (bool, error)
    ListAll(ctx context.Context) ([]Feature, error) // For cache population
    
    // Write operations
    // CreateFeature: Input Feature.ID may be empty (backend assigns) or FT-LOCAL-* (GitLab tracks)
    // Output Feature always has the canonical ID assigned by backend
    CreateFeature(ctx context.Context, feature Feature) (*Feature, error)
    UpdateFeature(ctx context.Context, id string, updates Feature) (*Feature, error)
    DeleteFeature(ctx context.Context, id string) error
    
    // Info
    Mode() string // "atlas" or "gitlab"
    GetAuthInfo(ctx context.Context) (*AuthInfo, error)
}

// Backend-agnostic errors
var (
    ErrNotFound       = errors.New("feature not found")
    ErrAlreadyExists  = errors.New("feature already exists")
    ErrInvalidID      = errors.New("invalid feature ID format")
    ErrPermission     = errors.New("permission denied")
    ErrBackendOffline = errors.New("backend not reachable")
    ErrConflict       = errors.New("conflict in concurrent update")
    ErrNotSupported   = errors.New("operation not supported")
)

```

### Factory Function (in `cmd/featctl/main.go`)

```go
// cmd/featctl/main.go
import (
    "github.com/JoobyPM/feature-atlas-service/internal/backend"
    "github.com/JoobyPM/feature-atlas-service/internal/backend/atlas"
    "github.com/JoobyPM/feature-atlas-service/internal/backend/gitlab"
    "github.com/JoobyPM/feature-atlas-service/internal/config"
)

// newBackend creates a backend based on configuration.
// Located in main.go to avoid circular imports (backend.go defines interface only).
func newBackend(cfg *config.Config) (backend.FeatureBackend, error) {
    switch cfg.Mode {
    case "atlas":
        return atlas.New(cfg.Atlas)
    case "gitlab":
        return gitlab.New(cfg.GitLab)
    default:
        return nil, fmt.Errorf("unknown mode: %s", cfg.Mode)
    }
}
```

### Backend Implementation Notes

**Atlas Backend Limitations:**
- `UpdateFeature()` → returns `ErrNotSupported` (server API doesn't support update)
- `DeleteFeature()` → returns `ErrNotSupported` (server API doesn't support delete)
- `ListAll()` → implemented via `Search(ctx, "", 1000)` with pagination if needed
- `GetAuthInfo()` → maps from `apiclient.ClientInfo`:
  - `ClientInfo.Name` → `AuthInfo.Username`
  - `ClientInfo.Name` → `AuthInfo.DisplayName` (same value)
  - `ClientInfo.Role` → `AuthInfo.Role` (compatible: "admin"/"user")

**GitLab Backend:**
- All operations supported
- Write operations return immediately after MR creation (async merge)
- `ListAll()` → fetches all files from `features/` directory via GitLab API

### Error Mapping

| Source (Atlas/Manifest) | Backend Error |
|-------------------------|---------------|
| `apiclient.ErrFeatureNotFound` | `backend.ErrNotFound` |
| `manifest.ErrIDExists` | `backend.ErrAlreadyExists` |
| `manifest.ErrInvalidID` | `backend.ErrInvalidID` |
| HTTP 403 Forbidden | `backend.ErrPermission` |
| HTTP 409 Conflict | `backend.ErrConflict` |
| Network timeout / connection refused | `backend.ErrBackendOffline` |
| GitLab 404 on file | `backend.ErrNotFound` |
| GitLab insufficient scope | `backend.ErrPermission` |

### Git Catalog Structure

```
feature-catalog-repo/
├── features/
│   ├── FT-000001.yaml
│   ├── FT-000002.yaml
│   └── ...
└── README.md
```

**Feature file format** (`features/FT-000001.yaml`):
```yaml
id: FT-000001
name: Authentication Flow
summary: End-to-end user authentication feature
owner: platform-team
tags: [backend, security]
created_at: "2026-01-22T10:00:00Z"
updated_at: "2026-01-22T10:00:00Z"  # Updated on each change (for conflict detection)
```

---

## Dependencies

### New Dependencies (Latest Stable)

| Package | Version | Purpose |
|---------|---------|---------|
| `gitlab.com/gitlab-org/api/client-go` | v1.16.0+ | Official GitLab API client |
| `github.com/zalando/go-keyring` | v0.2.6+ | Secure credential storage (OS keyring) |

> **Note:** Use `gitlab.com/gitlab-org/api/client-go` (official). The legacy `github.com/xanzy/go-gitlab` was archived Dec 2024 and should NOT be used.

### Existing Dependencies (No Changes)

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/spf13/cobra` | v1.10.2 | CLI framework |
| `github.com/charmbracelet/bubbletea` | v1.3.10 | TUI framework |
| `github.com/charmbracelet/huh` | v0.8.0 | Form components |

---

## Configuration Schema

### Config File (`~/.config/featctl/config.yaml`)

```yaml
# featctl configuration
version: 1
mode: "gitlab"  # or "atlas"

# Atlas mode settings (existing)
atlas:
  server_url: "https://localhost:8443"
  cert: "certs/alice.crt"
  key: "certs/alice.key"
  ca_cert: "certs/ca.crt"

# GitLab mode settings (new)
gitlab:
  instance: "https://gitlab.com"        # or self-hosted URL
  project: "mygroup/feature-catalog"    # path or numeric ID
  main_branch: "main"                   # source of truth branch
  
  # OAuth settings (required for interactive login)
  oauth_client_id: ""                   # GitLab OAuth Application ID
  # Note: Register app at GitLab > Settings > Applications
  #       Scopes: api (or read_api for read-only)
  #       Redirect URI: can be empty for device flow
  
  # MR settings
  mr_labels: ["feature"]
  mr_remove_source_branch: true
  
  # Optional: default assignee/reviewers
  default_assignee: ""                  # GitLab username

# Cache settings (applies to GitLab mode)
cache:
  ttl: "1h"                             # Cache TTL (Go duration: 1h, 30m, etc.)
  dir: ""                               # Custom cache dir (default: .fas/)
```

### Environment Variable Overrides

| Variable | Maps To |
|----------|---------|
| `FEATCTL_MODE` | `mode` |
| `FEATCTL_GITLAB_INSTANCE` | `gitlab.instance` |
| `FEATCTL_GITLAB_PROJECT` | `gitlab.project` |
| `FEATCTL_GITLAB_TOKEN` | GitLab access token (bypasses keyring) |
| `FEATCTL_GITLAB_CLIENT_ID` | `gitlab.oauth_client_id` |
| `FEATCTL_ATLAS_SERVER` | `atlas.server_url` |
| `FEATCTL_CACHE_TTL` | `cache.ttl` |
| `CI_JOB_TOKEN` | Auto-detected in GitLab CI (read-only) |

### Config Precedence (Highest to Lowest)

1. CLI flags (`--mode`, `--gitlab-project`, etc.)
2. Environment variables
3. Project config (`.featctl.yaml` in repo root)
4. Global config (`~/.config/featctl/config.yaml`)
5. Built-in defaults

### CLI Flags (New)

| Flag | Type | Description |
|------|------|-------------|
| `--mode` | string | Backend mode: `atlas` or `gitlab` |
| `--gitlab-instance` | string | GitLab instance URL |
| `--gitlab-project` | string | GitLab project path or ID |
| `--config` | string | Custom config file path |
| `--context` | string | Named context from config (for multi-instance) |

**Existing flags preserved:**
- `--server`, `--ca`, `--cert`, `--key` (Atlas mode)
- `--manifest`, `--sync`, `--dry-run` (manifest commands)

---

## Authentication

### Prerequisites: GitLab OAuth Application

Before using interactive login, register an OAuth application in GitLab:

1. Go to **GitLab** → **Settings** → **Applications** (user or group level)
2. Create application with:
   - **Name:** `featctl`
   - **Redirect URI:** (leave empty for device flow, or `http://localhost`)
   - **Confidential:** No (public client for CLI)
   - **Scopes:** `api` (full access) or `read_api` (read-only)
3. Copy the **Application ID** to config: `gitlab.oauth_client_id`

For self-hosted GitLab: Ensure GitLab version ≥17.2 (Device Authorization Grant support).

### Interactive Users: OAuth2 Device Authorization Grant

**Flow:**
1. User runs `featctl login --gitlab`
2. CLI calls GitLab `/oauth/authorize_device` with `client_id` from config
3. CLI displays: `"Go to https://gitlab.com/oauth/device and enter code XXXX"`
4. CLI polls `/oauth/token` until user approves (5s interval)
5. Token stored in OS keyring via `go-keyring` (keyed by instance URL)
6. Refresh tokens handled automatically (2h expiry, auto-refresh)

**Commands:**
```bash
featctl login --gitlab                    # Interactive OAuth2 device flow
featctl login --gitlab --token <PAT>      # Direct token (for scripts)
featctl logout --gitlab                   # Remove stored credentials
featctl auth status                       # Show auth status for all backends
```

### Machine-to-Machine (CI/CD)

| Scenario | Token Type | Scope | How to Provide |
|----------|-----------|-------|----------------|
| Read-only | Project Access Token (Reporter) | `read_api` | `FEATCTL_GITLAB_TOKEN` env var |
| Write (MRs) | Project Access Token (Developer) | `api` | `FEATCTL_GITLAB_TOKEN` env var |
| Same project CI | CI Job Token | (automatic) | `CI_JOB_TOKEN` auto-detected |

### Credential Storage

| Priority | Method | Use Case |
|----------|--------|----------|
| 1 | Environment variable | CI/CD, containers |
| 2 | OS Keyring (`go-keyring`) | Interactive users |
| 3 | Config file (chmod 600) | Headless fallback |

---

## Merge Request Workflow

### Branch Naming
```
feature/add-<name-slug>-<random4>     # New feature
feature/update-<id>-<name-slug>       # Update existing
feature/delete-<id>                   # Delete feature
```

### Commit Message (Conventional Commits)
```
feat: add feature <Name> (ID: FT-NNNNNN)
chore: update feature <Name> (ID: FT-NNNNNN)
chore: delete feature <Name> (ID: FT-NNNNNN)
```

### MR Title
```
Add feature: <Name> (ID: FT-NNNNNN)
Update feature: <Name> (ID: FT-NNNNNN)
Delete feature: <Name> (ID: FT-NNNNNN)
```

### MR Description Template
```markdown
## Feature Proposal

**Name:** Authentication Flow
**Summary:** End-to-end user authentication feature
**Owner:** platform-team
**Tags:** backend, security

## Checklist
- [x] YAML file added/updated under `features/`
- [ ] (Reviewer) Check for duplicates
- [ ] (Reviewer) Validate owner exists
```

### MR Settings
- `RemoveSourceBranch: true` (auto-cleanup)
- Labels: `["feature"]` (configurable)
- Assignee: Owner if valid GitLab user, else `default_assignee`
- No auto-merge (manual approval required)

---

## Sync Strategy

### Behavior Matrix

| Scenario | Default Action | `--force-local` |
|----------|---------------|-----------------|
| Local new feature (FT-LOCAL-*) | Create MR | Create MR |
| MR pending (not merged) | Skip, show status | Skip |
| MR merged | Update local ID → FT-NNNNNN, mark synced | — |
| Remote updated | Pull to local (overwrite) | Create update MR |
| Remote new (not in local) | Warn user | Warn user |
| Conflict (both changed) | Remote wins | Create update MR |

### Conflict Resolution Policy
- **Default:** Remote wins (GitLab is SST)
- **Override:** `--force-local` creates update MR to push local changes
- **Explicit:** `--force-remote` discards local changes without warning

### Pending MR Tracking
Local file `.fas/pending-mrs.json` tracks MRs created but not yet merged:
```json
{
  "version": "1",
  "pending": [
    {
      "local_id": "FT-LOCAL-auth-flow",
      "mr_iid": 42,
      "mr_url": "https://gitlab.com/group/repo/-/merge_requests/42",
      "branch": "feature/add-auth-flow-x1y2",
      "created_at": "2026-01-22T10:00:00Z"
    }
  ]
}
```
- Updated on `CreateFeature()` (add entry)
- Cleaned up on `sync` when MR is merged (remove entry, update manifest ID)
- Queried on `sync` to show pending status

---

## Implementation Phases

### Phase 1: Configuration System (Prerequisite)
**Deliverables:**
- [ ] `internal/config/config.go` — load/parse YAML config
- [ ] Environment variable overrides
- [ ] Config discovery (global → project → flags)
- [ ] `featctl config show` command

> **Note:** Config must come first because the backend factory needs `config.Config` to determine mode.

**Files:**
| File | Action |
|------|--------|
| `internal/config/config.go` | **NEW** |
| `internal/config/config_test.go` | **NEW** |
| `cmd/featctl/main.go` | Add config loading (before backend init) |

### Phase 2: Backend Interface & Atlas Adapter
**Deliverables:**
- [ ] `internal/backend/backend.go` — interface + types + error types
- [ ] `internal/backend/atlas/backend.go` — wraps `apiclient.Client`
- [ ] `internal/backend/mock/backend.go` — mock for testing
- [ ] Refactor `cmd/featctl/main.go` to use `newBackend()`
- [ ] Refactor `internal/tui/` to accept `FeatureBackend` (tui.go + form.go)
- [ ] Refactor `internal/cache/` to use `backend.Feature` for population
- [ ] All existing tests pass (no behavior change)

**Files:**
| File | Action |
|------|--------|
| `internal/backend/backend.go` | **NEW** — interface, Feature, SuggestItem, AuthInfo, errors |
| `internal/backend/atlas/backend.go` | **NEW** — wraps apiclient, translates types |
| `internal/backend/atlas/backend_test.go` | **NEW** — interface compliance tests |
| `internal/backend/mock/backend.go` | **NEW** — mock implementation for tests |
| `cmd/featctl/main.go` | Refactor to use `newBackend()` factory |
| `internal/tui/tui.go` | Accept `FeatureBackend`, use `backend.SuggestItem` |
| `internal/tui/tui_test.go` | Update tests to use mock backend |
| `internal/tui/form.go` | Accept `FeatureBackend`, use `backend.Feature` |
| `internal/tui/form_test.go` | Update tests to use mock backend |
| `internal/cache/cache.go` | Update `cache.CachedFeature` population from `backend.Feature` |

**Type Migration:**
- `apiclient.SuggestItem` → `backend.SuggestItem`
- `apiclient.Feature` → `backend.Feature`
- `tui.Result.Selected` type changes from `[]apiclient.SuggestItem` to `[]backend.SuggestItem`
- `cache.CachedFeature` populated from `backend.Feature` (fields unchanged)

**Impact on `cmd/featctl/main.go`:**
- `initClient()` → `initBackend()` using `newBackend(cfg)`
- `addSelectedToManifest()` parameter type changes
- All commands using `client.Xxx()` → `activeBackend.Xxx()`
- Global `var client *apiclient.Client` → `var activeBackend backend.FeatureBackend`

**Deprecated Code:**
- `tui.RunLegacy()` — remove after migration (was already deprecated)

### Phase 3: GitLab Authentication
**Deliverables:**
- [ ] `internal/auth/gitlab.go` — OAuth2 Device Flow
- [ ] `internal/auth/keyring.go` — credential storage via `go-keyring`
- [ ] `featctl login --gitlab` command
- [ ] `featctl logout --gitlab` command
- [ ] `featctl auth status` command
- [ ] Token refresh logic

**Files:**
| File | Action |
|------|--------|
| `internal/auth/gitlab.go` | **NEW** |
| `internal/auth/keyring.go` | **NEW** |
| `internal/auth/auth_test.go` | **NEW** |
| `cmd/featctl/main.go` | Add auth commands |
| `go.mod` | Add `zalando/go-keyring` |

### Phase 4: GitLab Backend (Read Operations)
**Deliverables:**
- [ ] `internal/backend/gitlab/backend.go` — GitLab implementation
- [ ] `Suggest()` — list features, filter by prefix
- [ ] `Search()` — full catalog search
- [ ] `GetFeature()` — read single feature file
- [ ] `FeatureExists()` — check file exists
- [ ] Local caching for offline hints

**Files:**
| File | Action |
|------|--------|
| `internal/backend/gitlab/backend.go` | **NEW** |
| `internal/backend/gitlab/backend_test.go` | **NEW** |
| `internal/backend/gitlab/cache.go` | **NEW** (optional) |
| `go.mod` | Add `gitlab.com/gitlab-org/api/client-go` |

### Phase 5: GitLab Backend (Write Operations)
**Deliverables:**
- [ ] `CreateFeature()` — branch + commit + MR workflow
- [ ] `UpdateFeature()` — edit existing feature via MR
- [ ] `DeleteFeature()` — remove/deprecate via MR
- [ ] ID assignment logic (FT-NNNNNN from max existing)
- [ ] MR description template generation
- [ ] Collision/conflict handling

**ID Assignment Algorithm:**
```go
// 1. List all feature files from main branch
// 2. Parse IDs, find max N in FT-NNNNNN
// 3. New ID = max + 1
// 4. Create branch with new feature file
// 5. If commit fails (file exists): increment ID, retry (max 3)
// 6. Create MR
```

**Files:**
| File | Action |
|------|--------|
| `internal/backend/gitlab/backend.go` | Add write methods |
| `internal/backend/gitlab/mr.go` | **NEW** — MR helper functions |
| `internal/backend/gitlab/id.go` | **NEW** — ID generation with retry |

### Phase 6: Sync Strategy
**Deliverables:**
- [ ] `featctl manifest sync` for GitLab mode
- [ ] Pull remote updates to local manifest
- [ ] Push local changes via MR
- [ ] Conflict detection and resolution
- [ ] `--force-local`, `--force-remote`, `--dry-run` flags
- [ ] Pending MR state tracking (local `.fas/pending-mrs.json`)

**Files:**
| File | Action |
|------|--------|
| `internal/backend/gitlab/sync.go` | **NEW** |
| `internal/backend/gitlab/pending.go` | **NEW** — track pending MRs locally |
| `cmd/featctl/main.go` | Extend manifest sync |

### Phase 7: Integration Testing & Documentation
**Deliverables:**
- [ ] Integration tests with GitLab API mocks
- [ ] E2E tests with test GitLab project
- [ ] README updates
- [ ] CLI help text updates
- [ ] Migration guide for existing users

**Files:**
| File | Action |
|------|--------|
| `test/integration/gitlab_test.go` | **NEW** — GitLab API mock tests |
| `test/integration/sync_test.go` | Update for dual-mode (Atlas + GitLab) |
| `test/integration/testutil/gitlab.go` | **NEW** — GitLab test helpers |
| `test/e2e/gitlab_test.go` | **NEW** — general GitLab E2E tests |
| `test/e2e/gitlab_mr_test.go` | **NEW** — MR workflow E2E tests |
| `test/e2e/tui_gitlab_test.go` | **NEW** — TUI with GitLab backend |
| `README.md` | Update |

---

## Acceptance Criteria

### Core Functionality
1. [ ] `featctl --mode gitlab` uses GitLab backend
2. [ ] `featctl --mode atlas` uses existing Atlas backend (no regression)
3. [ ] TUI works identically with both backends
4. [ ] Config file switches mode without code changes

### Authentication
5. [ ] `featctl login --gitlab` completes OAuth2 device flow
6. [ ] Token stored securely in OS keyring
7. [ ] Token auto-refreshes before expiry
8. [ ] `FEATCTL_GITLAB_TOKEN` env var bypasses interactive login
9. [ ] `featctl logout --gitlab` removes credentials

### Read Operations (GitLab)
10. [ ] `featctl search` lists features from GitLab repo
11. [ ] `featctl get <id>` retrieves feature from GitLab
12. [ ] TUI autocomplete works with GitLab features
13. [ ] Offline mode shows cached data with warning

### Write Operations (GitLab)
14. [ ] `featctl feature create` opens MR with new feature
15. [ ] MR follows naming conventions (branch, commit, title)
16. [ ] MR description includes feature details
17. [ ] Concurrent creates don't produce duplicate IDs
18. [ ] Failed MR leaves branch for manual recovery

### Sync (GitLab)
19. [ ] `featctl manifest sync` pushes unsynced features via MRs
20. [ ] Merged MRs update local manifest with official IDs
21. [ ] Remote updates pull to local manifest
22. [ ] `--force-local` creates update MRs for conflicts
23. [ ] `--dry-run` shows planned actions without executing

### Configuration
24. [ ] Global config (`~/.config/featctl/config.yaml`) loads
25. [ ] Project config (`.featctl.yaml`) overrides global
26. [ ] Environment variables override config files
27. [ ] CLI flags override everything

### Error Handling & Resilience
28. [ ] Backend errors correctly mapped (apiclient → backend errors)
29. [ ] Rate limiting respects GitLab `Retry-After` header
30. [ ] Exponential backoff on 429/503 responses (max 3 retries)
31. [ ] Offline mode uses cached data when GitLab unreachable
32. [ ] OAuth tokens stored with instance-specific key (multi-instance support)

---

## Test Strategy

| Layer | What to Test | Location |
|-------|--------------|----------|
| **Unit** | Backend interface compliance, ID generation, YAML parsing | `internal/backend/*_test.go` |
| **Unit** | Config loading, precedence, env overrides | `internal/config/*_test.go` |
| **Unit** | Auth flow state machine, token refresh | `internal/auth/*_test.go` |
| **Integration** | GitLab API calls (mocked) | `test/integration/gitlab_test.go` |
| **Integration** | Full sync workflow (mocked GitLab) | `test/integration/sync_test.go` |
| **E2E** | TUI with GitLab backend | `test/e2e/tui_gitlab_test.go` |
| **E2E** | Complete MR workflow (test project) | `test/e2e/gitlab_mr_test.go` |

### Test GitLab Project
- Create dedicated test project: `featctl-test/feature-catalog`
- Use Project Access Token for CI
- Clean up test branches/MRs after each test run

---

## Resilience & Rate Limiting

### Rate Limit Handling
```go
// Retry on these status codes:
// - 429 Too Many Requests (rate limit)
// - 503 Service Unavailable (temporary)
// - 500 Internal Server Error (may be transient)

// Strategy:
// - Check Retry-After header first
// - If no header: exponential backoff (1s, 2s, 4s)
// - Max 3 retries per operation
// - Log rate limit events for observability
```

### Offline Mode
- Cache feature list on successful fetch (stored in `.fas/` or `cache.dir`)
- If GitLab unreachable: use cache with warning message
- Cache TTL: 1 hour default (configurable via `cache.ttl` in config or `FEATCTL_CACHE_TTL` env)
- Cache invalidation: automatic on successful write operations

### Multi-Instance Cache Isolation
Cache is keyed by backend mode and instance URL:
- Atlas: `.fas/atlas-{server_hash}/features.json`
- GitLab: `.fas/gitlab-{instance_hash}/features.json`

This prevents cache pollution when switching between instances or modes.

---

## Risk Assessment

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| GitLab API rate limiting | Medium | Medium | Exponential backoff, respect Retry-After, caching |
| ID collision on concurrent MRs | High | Low | Retry with next ID, atomic file creation |
| Token expiry during long operation | Medium | Medium | Refresh token before each operation if <5min remaining |
| Breaking existing Atlas users | High | Low | Phase 1 is pure refactor, 100% test coverage required |
| Keyring unavailable in containers | Medium | High | Env var fallback documented, auto-detect headless |
| GitLab instance <17.2 (no device flow) | Low | Low | Document requirement, fallback to `--token` flag |
| OAuth app not registered | Medium | Medium | Clear error message with setup instructions |

---

## Out of Scope

- Batch operations (bulk create/update/delete)
- GitLab CI pipeline integration (webhook triggers)
- Feature deprecation workflow (soft delete)
- Multi-project catalog support
- GitLab group-level operations
- Automatic CODEOWNERS generation

---

## References

- [GitLab API Client (Go)](https://pkg.go.dev/gitlab.com/gitlab-org/api/client-go)
- [GitLab OAuth2 Device Flow](https://docs.gitlab.com/api/oauth2/#device-authorization-grant)
- [GitLab Token Best Practices](https://docs.gitlab.com/security/tokens/)
- [go-keyring Library](https://github.com/zalando/go-keyring)
- Deep Research: `tmp/deep_research_result.md`
