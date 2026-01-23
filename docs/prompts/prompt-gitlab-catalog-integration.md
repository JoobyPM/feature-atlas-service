# Prompt: GitLab Catalog Integration

## Task
Implement dual-mode backend for `featctl` per `docs/todo/SWD-gitlab-catalog-integration.md`.

**Objective:** Enable `featctl` to work with Atlas (mTLS) and GitLab (Git catalog + MR workflow) via unified `FeatureBackend` interface.

## Context
- **Spec:** `docs/todo/SWD-gitlab-catalog-integration.md`
- **Branch:** `feature/SWD-XXX-gitlab-catalog-integration` *(replace XXX with Jira ticket)*
- **New deps:** `gitlab.com/gitlab-org/api/client-go` v1.16.0+, `github.com/zalando/go-keyring` v0.2.6+

### Key Files (Study Before Coding)
- `cmd/featctl/main.go` — CLI entry, current apiclient usage, cobra flag patterns
- `internal/apiclient/client.go` — existing Atlas client (reference for backend structure)
- `internal/tui/tui.go`, `internal/tui/form.go` — TUI using apiclient types
- `internal/tui/tui_test.go` — uses `teatest` (follow same pattern for new tests)
- `internal/manifest/manifest.go` — local manifest structure
- `internal/cache/cache.go` — caching logic
- `test/integration/api_test.go` — integration test structure (reference)

### Architecture
```
        CLI / TUI (uses FeatureBackend)
                    │
        ┌───────────┴───────────┐
        │  internal/backend/    │
        │  FeatureBackend iface │
        └───────────┬───────────┘
                    │
     ┌──────────────┴──────────────┐
     │                             │
atlas/backend.go          gitlab/backend.go
(wraps apiclient)         (gitlab-org SDK)
```

---

## Implementation Phases

Execute sequentially. Each phase must pass `make check` before proceeding.

### Phase 1: Configuration System
**Files:** `internal/config/config.go` (NEW), `internal/config/config_test.go` (NEW), `cmd/featctl/main.go`

**Config:** YAML with `mode`, `atlas.*`, `gitlab.*`, `cache.*` sections.
**Precedence:** CLI flags > env vars > project `.featctl.yaml` > global `~/.config/featctl/config.yaml` > defaults.
**Env vars:** `FEATCTL_MODE`, `FEATCTL_GITLAB_*`, `FEATCTL_ATLAS_SERVER`, `FEATCTL_CACHE_TTL`, `CI_JOB_TOKEN`.
**Deliverable:** `featctl config show` prints resolved config.

### Phase 2: Backend Interface & Atlas Adapter
**Files:**
- `internal/backend/backend.go` (NEW) — interface + types + errors
- `internal/backend/atlas/backend.go` (NEW) — wraps apiclient
- `internal/backend/atlas/backend_test.go` (NEW) — interface compliance tests
- `internal/backend/mock/backend.go` (NEW) — for tests
- `cmd/featctl/main.go` — replace `client` with `activeBackend`
- `internal/tui/tui.go` — accept `FeatureBackend`, use `backend.SuggestItem`
- `internal/tui/tui_test.go` — update tests to use mock backend
- `internal/tui/form.go` — accept `FeatureBackend`, use `backend.Feature`
- `internal/tui/form_test.go` — update tests to use mock backend
- `internal/cache/cache.go` — use `backend.Feature`
- `internal/cache/cache_test.go` — update tests for new population logic

**Interface:**
```go
import ("context"; "errors"; "time")

type Feature struct {
    ID, Name, Summary, Owner string
    Tags                     []string
    CreatedAt, UpdatedAt     time.Time
}
type SuggestItem struct { ID, Name, Summary string }
type AuthInfo struct { Username, DisplayName, Role string }

type FeatureBackend interface {
    Suggest(ctx context.Context, query string, limit int) ([]SuggestItem, error)
    Search(ctx context.Context, query string, limit int) ([]Feature, error)
    GetFeature(ctx context.Context, id string) (*Feature, error)
    FeatureExists(ctx context.Context, id string) (bool, error)
    ListAll(ctx context.Context) ([]Feature, error)
    CreateFeature(ctx context.Context, feature Feature) (*Feature, error)
    UpdateFeature(ctx context.Context, id string, updates Feature) (*Feature, error)
    DeleteFeature(ctx context.Context, id string) error
    Mode() string
    GetAuthInfo(ctx context.Context) (*AuthInfo, error)
}

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

**Atlas notes:** `UpdateFeature()`/`DeleteFeature()` return `ErrNotSupported`.

**Refactoring:**
- `var client *apiclient.Client` → `var activeBackend backend.FeatureBackend`
- `initClient()` → `initBackend()` using factory
- All `client.Xxx()` → `activeBackend.Xxx()`
- Remove deprecated `tui.RunLegacy()`

**Deliverable:** All existing commands work unchanged with `--mode atlas`.

### Phase 3: GitLab Authentication
**Files:**
- `internal/auth/gitlab.go` (NEW) — OAuth2 Device Flow
- `internal/auth/keyring.go` (NEW) — go-keyring wrapper
- `internal/auth/auth_test.go` (NEW) — OAuth state machine, token refresh tests
- `cmd/featctl/main.go` — add login/logout/auth commands
- `go.mod` — add `github.com/zalando/go-keyring`

**Prerequisite (document for users):**
1. Register GitLab OAuth App: Settings → Applications
2. Name: `featctl`, Confidential: No, Scopes: `api` (or `read_api`)
3. Copy Application ID to `gitlab.oauth_client_id` in config

**OAuth2 Device Flow:**
1. POST `/oauth/authorize_device` with `client_id` from config
2. Display: "Go to https://gitlab.com/oauth/device and enter code XXXX"
3. Poll `/oauth/token` every 5s until approved
4. Store in OS keyring (keyed by instance URL)
5. Auto-refresh when <5min remaining

**Commands:** `featctl login --gitlab`, `featctl logout --gitlab`, `featctl auth status`
**Priority:** env var > keyring > config file (chmod 600)
**Deliverable:** `featctl login --gitlab` stores token for subsequent commands.

### Phase 4: GitLab Backend (Read)
**Files:**
- `internal/backend/gitlab/backend.go` (NEW) — main implementation
- `internal/backend/gitlab/backend_test.go` (NEW) — unit tests
- `internal/backend/gitlab/cache.go` (NEW) — optional, offline cache helpers
- `go.mod` — add `gitlab.com/gitlab-org/api/client-go`

**Catalog:** `features/FT-NNNNNN.yaml` files with `id`, `name`, `summary`, `owner`, `tags`, `created_at`, `updated_at`.

**Methods:**
- `Suggest()` — list files in `features/`, filter by prefix
- `Search()` — list all files, parse YAML, filter by name/summary
- `GetFeature()` — fetch single file by ID
- `FeatureExists()` — HEAD request to check file exists
- `ListAll()` — fetch all files from `features/` directory via Repository Tree API

**Caching:** `.fas/gitlab-{hash}/features.json`, TTL from config, invalidate on writes.
**Deliverable:** `featctl search --mode gitlab` returns features from GitLab.

### Phase 5: GitLab Backend (Write)
**Files:**
- `internal/backend/gitlab/backend.go` — add write methods (`CreateFeature`, `UpdateFeature`, `DeleteFeature`)
- `internal/backend/gitlab/mr.go` (NEW) — MR helper functions
- `internal/backend/gitlab/id.go` (NEW) — ID generation with retry

**ID Assignment:** Find max FT-NNNNNN, increment, retry on collision (max 3).
**MR Workflow:** Create branch → commit YAML → create MR with title/labels/description template.
**Branch naming:** `feature/add-<slug>-<rand>`, `feature/update-<id>-<slug>`, `feature/delete-<id>`
**Deliverable:** `featctl feature create --mode gitlab` opens MR.

### Phase 6: Sync Strategy
**Files:**
- `internal/backend/gitlab/sync.go` (NEW) — sync logic
- `internal/backend/gitlab/pending.go` (NEW) — track pending MRs locally
- `cmd/featctl/main.go` — extend manifest sync command

**Pending tracking:** `.fas/pending-mrs.json` — local_id, mr_iid, mr_url, branch, created_at.
**Sync rules:**
- Local new → create MR
- MR merged → update local ID
- Remote updated → pull (or `--force-local` creates update MR)
- Conflict → remote wins (default)

**Flags:** `--force-local`, `--force-remote`, `--dry-run`
**Deliverable:** `featctl manifest sync --mode gitlab` reconciles local and remote.

### Phase 7: Testing & Docs
**Files:**
- `test/integration/gitlab_test.go` (NEW) — GitLab API mock tests
- `test/integration/sync_test.go` — update for dual-mode (Atlas + GitLab)
- `test/integration/testutil/gitlab.go` (NEW) — GitLab test helpers
- `test/e2e/gitlab_test.go` (NEW) — general GitLab E2E tests
- `test/e2e/gitlab_mr_test.go` (NEW) — MR workflow E2E tests
- `test/e2e/tui_gitlab_test.go` (NEW) — TUI with GitLab backend
- `README.md` — update with GitLab mode docs

**Test project:** `featctl-test/feature-catalog` with Project Access Token.
**Coverage:** Unit (interface, config, auth), Integration (mocked API), E2E (TUI + MR workflow).

---

## Requirements (32 Acceptance Criteria)

### Core (1-4)
`--mode gitlab` uses GitLab • `--mode atlas` unchanged • TUI identical both modes • Config switches mode

### Auth (5-9)
OAuth2 device flow works • Token in keyring • Auto-refresh • Env var bypasses login • Logout removes creds

### Read (10-13)
Search lists from GitLab • Get retrieves feature • TUI autocomplete works • Offline shows cached

### Write (14-18)
Create opens MR • MR naming conventions • MR description template • No duplicate IDs • Failed MR leaves branch

### Sync (19-23)
Sync pushes via MRs • Merged MRs update local ID • Remote pulls to local • `--force-local` creates update MRs • `--dry-run` shows actions

### Config (24-27)
Global config loads • Project overrides global • Env vars override config • CLI flags override all

### Resilience (28-32)
Errors mapped correctly • Rate limit respects Retry-After • Exponential backoff (3 retries) • Offline uses cache • Multi-instance cache isolation

---

## Constraints
- **DRY/SST:** All ops through `FeatureBackend` interface
- **No circular imports:** Factory in `main.go`, interface in `backend.go`
- **Backward compatible:** Atlas mode identical behavior
- **Error mapping:** All errors → `backend.Err*`
- **Official SDK only:** `gitlab.com/gitlab-org/api/client-go` (NOT xanzy/go-gitlab)
- **GitLab version:** ≥17.2 required for OAuth2 Device Flow (fallback: `--token` flag)

### CLI Flags (New)
| Flag | Description |
|------|-------------|
| `--mode` | Backend: `atlas` or `gitlab` |
| `--gitlab-instance` | GitLab instance URL |
| `--gitlab-project` | GitLab project path or ID |
| `--config` | Custom config file path |
| `--context` | Named context from config |

---

## Workflow
```
1. EXPLORE — Read existing code (apiclient, tui, manifest, tests).
2. PLAN — Review interface in todo doc. Identify all file changes.
3. CODE — One phase at a time. Tests alongside. `make check` after each file.
4. COMMIT — Conventional: `feat(backend): add FeatureBackend interface`. Ref: SWD-XXX.
```

**Thinking Levels:**
- Phase 1 (Config): `think hard` — standard multi-file
- Phase 2 (Interface + Refactor): `ultrathink` — critical, breaking change risk
- Phase 3 (Auth): `think hard` — security-sensitive
- Phase 4 (Read): `think hard` — GitLab API integration
- Phase 5 (Write): `think harder` — ID collision, MR workflow complexity
- Phase 6 (Sync): `think harder` — conflict resolution, state management
- Phase 7 (Tests): `think hard` — ensure coverage

---

## Definition of Done
- [ ] All 32 acceptance criteria pass
- [ ] `make fmt && make check` passes
- [ ] Unit tests: interface, config, auth
- [ ] Integration tests: GitLab API (mocked)
- [ ] E2E tests: TUI + MR workflow
- [ ] README updated
- [ ] No new lint errors
- [ ] MR approved and merged

---

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Rate limiting | Exponential backoff, `Retry-After`, caching |
| ID collision | Retry with increment (max 3) |
| Token expiry | Refresh if <5min remaining |
| Breaking Atlas | Phase 2 pure refactor, 100% coverage |
| No keyring | Env var fallback, detect headless |
| GitLab <17.2 | Document requirement, fallback to `--token` flag |
| OAuth app not registered | Clear error message with setup instructions |

---

## Out of Scope
Batch ops • GitLab CI webhooks • Feature deprecation (soft delete) • Multi-project • Group-level ops • CODEOWNERS generation
