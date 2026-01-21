# PRD: Local Manifest for Feature Catalog

## Overview

Enable offline-first feature catalog management through a local manifest file. Users can track project-specific features, create new features locally, and optionally sync with the remote service.

## Problem Statement

Currently, `featctl` requires a live connection to the server for all operations. This creates friction for:
- Offline development workflows
- CI/CD pipelines without service access
- Projects that need to reference features before server registration
- Teams wanting to propose new features locally before committing to the shared catalog

## Goals

1. Enable offline feature validation via local manifest
2. Support local feature creation with deferred sync
3. Maintain server as authoritative source of truth
4. Minimize friction in existing workflows

## Non-Goals

- Full offline replication of the entire server catalog
- Conflict resolution for concurrent edits (server wins)
- Version history or change tracking in manifest
- Multi-manifest support within a single project

## User Stories

**US-1**: As a developer, I want to validate feature references offline so I can work without server connectivity.

**US-2**: As a developer, I want to create a new feature locally so I can reference it in my code before it exists on the server.

**US-3**: As a team lead, I want to sync local features to the server so the team has a shared source of truth.

**US-4**: As a developer, I want to add existing server features to my local manifest so I can work offline.

## Solution Design

### Manifest File

**Location Discovery** (in order):
1. `--manifest` flag value (if provided)
2. `.feature-atlas.yaml` in current working directory
3. Walk up directory tree to git root (`.git` directory), checking each level
4. Stop at filesystem root; fail if not found

**File Format**: YAML (consistent with existing `.golangci.yml`, `lint` output)

**Schema Version**: `1` (stable; future changes require explicit `featctl manifest migrate`)

### Data Types

**ManifestEntry** (wraps `store.Feature` with sync metadata):

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | Feature identifier |
| `name` | string | yes | Human-readable name |
| `summary` | string | yes | Brief description |
| `owner` | string | no | Responsible team/person |
| `tags` | []string | no | Categorization labels |
| `synced` | bool | yes | Server sync status |
| `synced_at` | RFC3339 | if synced | Timestamp of last sync |
| `alias` | string | no | Original local ID (after sync renames) |

Note: `created_at` from `store.Feature` is server-assigned; omitted in manifest.

### ID Convention

| Source | Format | Regex | Example |
|--------|--------|-------|---------|
| Server | `FT-NNNNNN` | `^FT-[0-9]{6}$` | `FT-000123` |
| Local | `FT-LOCAL-*` | `^FT-LOCAL-[a-z0-9-]{1,64}$` | `FT-LOCAL-auth-flow` |

**Local ID Rules**:
- Prefix: `FT-LOCAL-` (required, case-sensitive)
- Suffix: lowercase letters, digits, hyphens only
- Length: 1-64 characters after prefix
- No leading/trailing hyphens in suffix

### Concurrency

**File Locking**: Advisory lock using OS-level `flock()` during manifest writes.
- Lock file: manifest path (lock on file descriptor, not separate `.lock` file)
- Timeout: 5 seconds; fail with "manifest locked by another process"
- Read operations do not acquire lock (snapshot consistency acceptable)

### CLI Commands

#### `featctl manifest init`

Create empty manifest in current directory.

**Flags**:
- `--force`: Overwrite existing manifest

**Behavior**:
- Creates `.feature-atlas.yaml` with `version: "1"` and empty `features` map
- Fails if manifest exists (unless `--force`)
- Does not acquire lock (atomic write via temp file + rename)

**Exit Codes**: 0 success, 1 already exists, 2 write error

#### `featctl manifest list`

List features in local manifest.

**Flags**:
- `--manifest`: Custom manifest path
- `--output`: Format (`text`, `json`, `yaml`); default `text`
- `--unsynced`: Show only unsynced features

**Behavior**:
- Reads manifest and displays features
- Text output shows sync status indicator: `[synced]` or `[local]`

**Exit Codes**: 0 success, 1 manifest not found, 2 parse error

#### `featctl feature create`

Create new feature in local manifest.

**Flags**:
- `--manifest`: Custom manifest path
- `--id`: Local ID (required, must match `^FT-LOCAL-[a-z0-9-]{1,64}$`)
- `--name`: Feature name (required)
- `--summary`: Feature summary (required)
- `--owner`: Feature owner (optional)
- `--tags`: Comma-separated tags (optional)

**Behavior**:
- Validates ID format strictly
- Fails if ID already exists in manifest
- Adds feature with `synced: false`
- Acquires file lock during write

**Exit Codes**: 0 success, 1 validation error, 2 ID exists, 3 write error

#### `featctl manifest add <id>` *(Phase 2)*

Add existing server feature to local manifest.

**Flags**:
- `--manifest`: Custom manifest path

**Behavior**:
- Fetches feature from server by ID (requires mTLS)
- Adds to manifest with `synced: true`, `synced_at: <now>`
- Idempotent: skips if ID already in manifest

**Exit Codes**: 0 success, 1 not found on server, 2 network error, 3 write error

#### `featctl manifest sync` *(Phase 2)*

Sync unsynced local features to server.

**Flags**:
- `--manifest`: Custom manifest path
- `--dry-run`: Show what would be synced without changes

**Behavior**:
- Requires admin client certificate
- For each unsynced feature:
  1. POST to `/admin/v1/features`
  2. Server assigns canonical `FT-NNNNNN` ID
  3. Update manifest: new ID, `synced: true`, `synced_at: <now>`
  4. Preserve original ID in `alias` field
- Atomic per-feature: partial sync on error; re-run continues

**Exit Codes**: 0 all synced, 1 partial failure (some synced), 2 auth error, 3 network error

### Modified Commands

#### `featctl lint` *(Phase 3)*

**Current**: Validates `feature_id` against server only.

**New**: Validates against local manifest first, falls back to server.

**Resolution Order**:
1. Load manifest (if exists and readable)
2. For each `feature_id` in target files:
   - Check manifest → return valid if found
   - Check server → return valid if found
   - Report error if not found in either
3. If manifest missing/unreadable: server-only (current behavior)

**New Flags**:
- `--manifest`: Custom manifest path
- `--offline`: Only check manifest; fail if ID not found locally

**Exit Codes**: 0 all valid, 1 validation errors, 2 manifest parse error (with `--offline`)

### Manifest Schema

```yaml
version: "1"
features:
  FT-000123:
    name: "User Authentication"
    summary: "OAuth2 and JWT-based authentication flow"
    owner: "Platform Team"
    tags:
      - auth
      - security
    synced: true
    synced_at: "2026-01-21T10:00:00Z"
  FT-000456:
    name: "Billing V2"
    summary: "New subscription billing system"
    owner: "Payments Team"
    tags:
      - billing
      - payments
    synced: true
    synced_at: "2026-01-21T11:30:00Z"
    alias: "FT-LOCAL-billing-v2"
  FT-LOCAL-new-dashboard:
    name: "Analytics Dashboard"
    summary: "Real-time metrics visualization"
    owner: "Data Team"
    tags:
      - analytics
      - frontend
    synced: false
```

## API Changes *(Phase 2)*

### New Admin Endpoint

`POST /admin/v1/features`

**Authorization**: Admin role required (same as `/admin/v1/clients`)

**Request Body**:
```json
{
  "name": "Feature Name",
  "summary": "Feature description",
  "owner": "Team Name",
  "tags": ["tag1", "tag2"]
}
```

**Response** (201 Created):
```json
{
  "id": "FT-000789",
  "name": "Feature Name",
  "summary": "Feature description",
  "owner": "Team Name",
  "tags": ["tag1", "tag2"],
  "created_at": "2026-01-21T12:00:00Z"
}
```

**Errors**:
- 400: Missing required fields (name, summary)
- 401: No client certificate
- 403: Not admin role
- 409: Duplicate name (if enforced)

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| Manifest not found | Commands requiring it fail with exit 1 and clear message |
| Manifest empty (no features) | Valid state; lint falls back to server |
| Invalid YAML | Fail with parse error, line number, column |
| ID format invalid | Reject with specific format requirements |
| ID collision local vs server | Impossible by design (`FT-LOCAL-` prefix) |
| Sync interrupted | Partial progress saved; re-run continues |
| Concurrent writes | Second writer waits up to 5s, then fails |
| Manifest in parent directory | Found via discovery; commands operate on it |

## Security Considerations

- `manifest sync` requires admin certificate (enforced by server)
- Manifest may contain sensitive feature names
- Recommended: add `.feature-atlas.yaml` to `.gitignore` for private projects
- No credentials stored in manifest
- File permissions: created with 0644 (user read/write, others read)

## Implementation Phases

### Phase 1: Core Manifest ✅
- [x] `internal/manifest` package (types, read/write, validation, locking)
- [x] `featctl manifest init` command
- [x] `featctl manifest list` command
- [x] `featctl feature create` command (local only, no `--sync`)
- [x] Unit tests for manifest package

### Phase 2: Server Integration ✅
- [x] `POST /admin/v1/features` endpoint
- [x] `featctl manifest add` command
- [x] `featctl manifest sync` command
- [x] Integration tests for sync workflow

### Phase 3: Lint Integration ✅
- [x] Modify `lint` to check manifest first
- [x] Add `--offline` and `--manifest` flags to `lint`
- [x] E2E tests for offline lint workflow

## Testing Strategy

| Test Type | Scope | Key Cases |
|-----------|-------|-----------|
| Unit | `internal/manifest` | Parse valid/invalid YAML, ID validation, locking |
| Unit | Commands | Flag parsing, error messages, exit codes |
| Integration | File I/O | Manifest discovery, concurrent access |
| Integration | Server sync | Auth, feature creation, ID assignment |
| E2E | Full workflow | init → create → sync → lint → verify |

## Success Metrics

1. `lint --offline` works without network (manifest-only validation)
2. Feature create → sync round-trip: 3 commands (`init`, `create`, `sync`)
3. No breaking changes to existing `lint` behavior (no manifest = server-only)
4. File lock prevents corruption under concurrent access

## Decisions Log

| Issue | Decision | Rationale |
|-------|----------|-----------|
| ID format | `^FT-LOCAL-[a-z0-9-]{1,64}$` | Prevents collision, easy validation |
| Manifest discovery | CWD → git root | Matches `.golangci.yml` pattern |
| Sync ID preservation | `alias` field | Machine-readable, supports refactoring |
| Concurrency | `flock()` on manifest | OS-level, no external deps |
| Phase 1 scope | No `--sync` on create | Avoids Phase 2 API dependency |

## References

- `store.Feature` struct: `internal/store/store.go:36`
- Existing lint command: `cmd/featctl/main.go`
- YAML library: `gopkg.in/yaml.v3` (already in go.mod)
- File locking: `golang.org/x/sys/unix.Flock` or `syscall.Flock`
