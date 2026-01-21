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

**Location**: `.feature-atlas.yaml` in project root (configurable via `--manifest` flag)

**Structure**:
- `version`: Manifest schema version
- `features`: Map of feature ID to feature data
- `metadata`: Sync timestamps, source info

**Feature Entry Fields**:
- `id`: Feature identifier
- `name`: Human-readable name
- `summary`: Brief description
- `owner`: Responsible team/person
- `tags`: Categorization labels
- `synced`: Boolean indicating server sync status
- `synced_at`: Timestamp of last sync (if synced)

### ID Convention

| Source | Format | Example |
|--------|--------|---------|
| Server | `FT-NNNNNN` | `FT-000123` |
| Local (unsynced) | `FT-LOCAL-*` | `FT-LOCAL-auth-flow` |

Local IDs are user-specified with `FT-LOCAL-` prefix. Upon sync, server assigns canonical ID.

### CLI Commands

#### `featctl manifest init`

Create empty manifest in current directory.

**Flags**:
- `--force`: Overwrite existing manifest

**Behavior**:
- Creates `.feature-atlas.yaml` with empty features map
- Fails if manifest exists (unless `--force`)

#### `featctl manifest add <id>`

Add existing server feature to local manifest.

**Flags**:
- `--manifest`: Custom manifest path

**Behavior**:
- Fetches feature from server by ID
- Adds to local manifest with `synced: true`
- Fails if feature not found on server
- Skips if already in manifest (idempotent)

#### `featctl manifest list`

List features in local manifest.

**Flags**:
- `--manifest`: Custom manifest path
- `--output`: Format (text, json, yaml)
- `--unsynced`: Show only unsynced features

**Behavior**:
- Reads manifest and displays features
- Shows sync status indicator

#### `featctl manifest sync`

Sync local features to server.

**Flags**:
- `--manifest`: Custom manifest path
- `--dry-run`: Show what would be synced without changes

**Behavior**:
- Requires admin client certificate
- Iterates unsynced features
- Creates each on server (server assigns canonical ID)
- Updates manifest with server ID and `synced: true`
- Preserves `FT-LOCAL-*` as alias in manifest comments

#### `featctl feature create`

Create new feature in local manifest.

**Flags**:
- `--manifest`: Custom manifest path
- `--id`: Local ID (required, must start with `FT-LOCAL-`)
- `--name`: Feature name (required)
- `--summary`: Feature summary (required)
- `--owner`: Feature owner
- `--tags`: Comma-separated tags
- `--sync`: Immediately sync to server

**Behavior**:
- Validates ID format (`FT-LOCAL-*`)
- Adds feature to manifest with `synced: false`
- If `--sync`, calls server API and updates manifest

### Modified Commands

#### `featctl lint`

**Current**: Validates `feature_id` against server only.

**New**: Validates against local manifest first, falls back to server.

**Resolution Order**:
1. Check local manifest (if exists)
2. If not found locally, check server
3. Report error if not found in either

**New Flags**:
- `--manifest`: Custom manifest path
- `--offline`: Only check local manifest (fail if not found locally)

### Manifest Schema

```yaml
version: "1"
features:
  FT-000123:
    name: "User Authentication"
    summary: "OAuth2 and JWT-based authentication flow"
    owner: "Platform Team"
    tags: ["auth", "security"]
    synced: true
    synced_at: "2026-01-21T10:00:00Z"
  FT-LOCAL-billing-v2:
    name: "Billing V2"
    summary: "New subscription billing system"
    owner: "Payments Team"
    tags: ["billing", "payments"]
    synced: false
```

## API Changes

### New Admin Endpoint

`POST /admin/v1/features` - Create new feature on server

**Request**:
- `name`: string (required)
- `summary`: string (required)
- `owner`: string
- `tags`: string array

**Response**:
- `id`: Server-assigned ID
- Full feature object

This endpoint enables `manifest sync` to create features without direct DB access.

## Edge Cases

| Scenario | Behavior |
|----------|----------|
| Manifest doesn't exist | Commands that require it fail with clear error |
| Feature ID collision (local vs server) | `FT-LOCAL-*` prefix prevents collision |
| Sync fails mid-way | Partial sync; re-run continues from unsynced |
| Server unreachable during sync | Fail with error; manifest unchanged |
| Manifest has invalid YAML | Fail with parse error and line number |
| `lint --offline` with no manifest | Fail with "no manifest found" error |

## Security Considerations

- `manifest sync` requires admin certificate (same as `admin/v1/clients`)
- Local manifest may contain sensitive feature names; add to `.gitignore` template
- No credentials stored in manifest

## Success Metrics

1. `lint` works offline with local manifest
2. Feature creation → sync round-trip under 5 commands
3. No breaking changes to existing `lint` behavior (server-only still works)

## Implementation Phases

### Phase 1: Core Manifest (This PR)
- `manifest init`
- `manifest list`
- `feature create` (local only)
- Manifest file read/write

### Phase 2: Server Integration
- `manifest add`
- `manifest sync`
- `POST /admin/v1/features` endpoint

### Phase 3: Lint Integration
- Modify `lint` to check local manifest first
- Add `--offline` and `--manifest` flags

## Testing Strategy

| Test Type | Coverage |
|-----------|----------|
| Unit | Manifest parsing, ID validation, feature CRUD |
| Integration | CLI commands, file I/O, server sync |
| E2E | Full workflow: init → create → sync → lint |

## Open Questions

1. Should `manifest add` support glob patterns (`FT-0001*`)?
2. Should we support manifest includes for monorepos?
3. Should sync preserve local ID as an alias field?

## References

- Existing `lint` command: `cmd/featctl/main.go:172`
- Feature struct: `internal/store/store.go:36`
- YAML library: `gopkg.in/yaml.v3`
