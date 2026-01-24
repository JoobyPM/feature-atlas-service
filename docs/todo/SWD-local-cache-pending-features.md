# Feature: Local Cache with Pending Feature Support

## Overview

Implement local cache that tracks pending MRs and their status, enabling developers to work offline while CI ensures only approved features are used in production.

## Jira Reference

- Ticket: SWD-XXX (TBD)
- Link: TBD

## Definition of Ready (DoR)

- [x] Requirements are clear and understood
- [x] Dependencies identified (existing pending.go, backend interface)
- [x] Technical approach discussed/approved
- [x] Acceptance criteria defined
- [x] Test strategy determined

## Definition of Done (DoD)

- [ ] Code implemented and reviewed
- [ ] Acceptance tests written and passing
- [ ] Unit/integration tests cover critical paths
- [ ] Documentation updated (separate workflow doc)
- [ ] No new linting errors
- [ ] MR approved and merged

## Acceptance Criteria

### 1. Enhanced Feature Model

- [ ] Add `Domain` field (required, at least 1)
- [ ] Add `Component` field (optional)
- [ ] Domain and Component are validated entities
- [ ] Update feature file format to include new fields

### 2. Local Cache with Pending Features

- [ ] Store full feature data (ID, name, summary, owner, tags, domain, component)
- [ ] Track MR URL and status (pending, merged, closed, conflict)
- [ ] Auto-refresh status on every search/tui command
- [ ] Persist cache to `.fas/` directory

### 3. Search with Pending Indicator

- [ ] Include pending features in search results
- [ ] Show `[PENDING]` indicator for unmerged features
- [ ] Show `[CLOSED]` indicator for closed/rejected MRs
- [ ] Show `[CONFLICT]` indicator for MRs with conflicts

### 4. TUI Enhancements

- [ ] Different color/icon for pending features (yellow/⏳)
- [ ] Different color/icon for closed features (red/✗)
- [ ] Different color/icon for conflict features (orange/⚠)
- [ ] Warning when selecting pending feature for manifest

### 5. Duplicate Detection (Online Mode)

- [ ] Validate unique name before creation (with loading indicator)
- [ ] Calculate name similarity with existing features
- [ ] Show confirmation dialog with similar features listed
- [ ] Block creation of exact duplicates

### 6. CI Validation

- [ ] Add `--strict` flag to lint command
- [ ] Validate all manifest features exist in merged catalog (main branch)
- [ ] Allow pending features locally, fail with `--strict`
- [ ] Support auth via environment variables for CI
- [ ] Document setup for GitLab CI/CD
- [ ] Document setup for GitHub Actions

### 7. MR Lifecycle Management

- [ ] Auto-update cache when MR is merged (keep same ID)
- [ ] Remove from cache and warn when MR is closed/rejected
- [ ] Show error when MR has conflicts
- [ ] `featctl pending` command to list/manage pending MRs

### 8. Documentation

- [ ] Create workflow documentation (separate doc)
- [ ] Document local development flow
- [ ] Document CI/CD integration
- [ ] Update README with workflow overview

### 9. Dogfooding (This Project)

- [ ] Create `features/` directory structure
- [ ] Add sample feature catalog entries
- [ ] TODO: Add CI pipeline check (future task)

## Technical Notes

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Local Developer                        │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐    ┌──────────────┐    ┌───────────────┐  │
│  │   featctl   │───▶│ Local Cache  │◀───│ GitLab API    │  │
│  │   tui/cli   │    │ (.fas/)      │    │ (MR Status)   │  │
│  └─────────────┘    └──────────────┘    └───────────────┘  │
│         │                  │                    │           │
│         ▼                  ▼                    ▼           │
│  ┌─────────────┐    ┌──────────────┐    ┌───────────────┐  │
│  │  Manifest   │    │ Pending MRs  │    │ features/     │  │
│  │ (features.y │    │ (cache.json) │    │ (main branch) │  │
│  └─────────────┘    └──────────────┘    └───────────────┘  │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                        CI Pipeline                          │
├─────────────────────────────────────────────────────────────┤
│  featctl lint --strict                                      │
│    ├── Check manifest features exist in features/ (main)    │
│    ├── FAIL if any feature is pending (not merged)          │
│    └── PASS if all features are approved                    │
└─────────────────────────────────────────────────────────────┘
```

### MR Status Enum

```go
type MRStatus string

const (
    MRStatusPending  MRStatus = "pending"   // MR open, awaiting review
    MRStatusMerged   MRStatus = "merged"    // MR merged, feature approved
    MRStatusClosed   MRStatus = "closed"    // MR closed/rejected
    MRStatusConflict MRStatus = "conflict"  // MR has merge conflicts
)
```

### Feature File Format (Updated)

```yaml
id: FT-000001
name: User Authentication
summary: OAuth2 and JWT-based authentication system
owner: platform-team
domain: security           # NEW: Required, at least 1
component: auth-service    # NEW: Optional
tags:
  - authentication
  - security
  - oauth2
```

### Cache File Format (.fas/cache.json)

```json
{
  "version": "2",
  "instance": "gitlab:gitlab.com/org/project",
  "updated_at": "2026-01-23T12:00:00Z",
  "features": [
    {
      "id": "FT-000001",
      "name": "User Authentication",
      "summary": "...",
      "owner": "platform-team",
      "domain": "security",
      "component": "auth-service",
      "tags": ["auth"],
      "status": "merged",
      "mr_url": null
    },
    {
      "id": "FT-000002",
      "name": "Feature Catalog",
      "summary": "...",
      "status": "pending",
      "mr_iid": 123,
      "mr_url": "https://gitlab.com/org/project/-/merge_requests/123"
    }
  ]
}
```

### Dependencies

- Existing: `internal/backend/gitlab/pending.go`
- Existing: `internal/cache/cache.go`
- New: Enhanced `MRStatus` tracking
- New: Domain/Component validation

## Test Strategy

- **E2E tests**: TUI with pending features display
- **Integration tests**: MR status refresh, cache persistence
- **Unit tests**: Status enum, duplicate detection, similarity calculation

## Implementation Order

1. Add Domain/Component to Feature model
2. Enhance PendingMR with status tracking
3. Create unified local cache (merge pending.go + cache.go concepts)
4. Update Search to include pending features
5. Update TUI with status indicators
6. Add duplicate detection with similarity
7. Add `--strict` flag for CI
8. Create workflow documentation
9. Dogfood: Create features/ directory for this project
