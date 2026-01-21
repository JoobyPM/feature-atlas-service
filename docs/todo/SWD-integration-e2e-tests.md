# Feature: Integration & E2E Tests with Testcontainers

## Overview

Complete remaining test items from `docs/prd/local-manifest.md`:
- **Phase 2**: Integration tests for sync workflow
- **Phase 3**: E2E tests for offline lint workflow

Use [testcontainers-go](https://golang.testcontainers.org/) for containerized server and [testify](https://github.com/stretchr/testify) for assertions.

## Jira Reference
- Ticket: SWD-XXX (TBD)

## Definition of Ready (DoR)
- [x] PRD approved and phases 1-3 implemented
- [x] Docker image builds successfully (`make docker-build`)
- [x] Certificate generation works (`scripts/gen-certs.sh`)
- [x] All unit tests passing (`make test`)

## Definition of Done (DoD)
- [ ] Integration tests cover sync workflow (Phase 2)
- [ ] E2E tests cover offline lint workflow (Phase 3)
- [ ] Tests run in CI (`make test-integration`)
- [ ] No flaky tests
- [ ] Documentation updated

## Technical Approach

### Dependencies to Add

```
github.com/stretchr/testify v1.9.0
github.com/testcontainers/testcontainers-go v0.35.0
```

### Test Structure

```
test/
├── integration/
│   ├── testutil/
│   │   ├── container.go      # Server container setup
│   │   ├── certs.go          # Certificate generation helpers
│   │   └── client.go         # API client helpers
│   ├── sync_test.go          # manifest sync workflow tests
│   └── api_test.go           # Admin API tests (POST /admin/v1/features)
└── e2e/
    └── lint_test.go          # Offline lint workflow tests
```

### Container Setup Strategy

1. **Base image**: Use pre-built `feature-atlasd` Docker image
2. **Certificates**: Generate temp certs per test suite (or reuse)
3. **Ports**: Use dynamic port allocation
4. **Cleanup**: Auto-cleanup via testcontainers lifecycle

## Implementation Plan

### Phase A: Infrastructure Setup

| Task | Description |
|------|-------------|
| A1 | Add testcontainers-go and testify to go.mod |
| A2 | Create `test/integration/testutil/container.go` - server container wrapper |
| A3 | Create `test/integration/testutil/certs.go` - temp certificate generation |
| A4 | Create `test/integration/testutil/client.go` - mTLS client helper |
| A5 | Add `test-integration` target to Makefile |

### Phase B: Integration Tests (PRD Phase 2)

| Task | Test Case | Description |
|------|-----------|-------------|
| B1 | `TestAdminCreateFeature` | POST /admin/v1/features creates feature with assigned ID |
| B2 | `TestAdminCreateFeature_Unauthorized` | Non-admin cert returns 403 |
| B3 | `TestAdminCreateFeature_BadRequest` | Missing name/summary returns 400 |
| B4 | `TestManifestAdd_FromServer` | `manifest add <id>` fetches and saves to manifest |
| B5 | `TestManifestAdd_NotFound` | `manifest add` with unknown ID returns error |
| B6 | `TestManifestSync_SingleFeature` | Sync one local feature to server |
| B7 | `TestManifestSync_MultipleFeatures` | Sync multiple features, verify IDs assigned |
| B8 | `TestManifestSync_Alias` | Verify original ID preserved in `alias` field |
| B9 | `TestManifestSync_PartialFailure` | Some features fail, others succeed |
| B10 | `TestManifestSync_DryRun` | `--dry-run` shows plan without changes |

### Phase C: E2E Tests (PRD Phase 3)

| Task | Test Case | Description |
|------|-----------|-------------|
| C1 | `TestLint_ManifestFirst` | Lint checks manifest before server |
| C2 | `TestLint_FallbackToServer` | Unknown ID in manifest checked against server |
| C3 | `TestLint_Offline_Valid` | `--offline` validates against manifest only |
| C4 | `TestLint_Offline_NotFound` | `--offline` fails if ID not in manifest |
| C5 | `TestLint_ManifestPath` | `--manifest` flag specifies custom path |
| C6 | `TestLint_NoManifest_ServerOnly` | No manifest falls back to server-only |
| C7 | `TestLint_LocalFeature` | Lint accepts `FT-LOCAL-*` IDs from manifest |

## Test Scenarios Detail

### B6: TestManifestSync_SingleFeature

```
Given: Manifest with FT-LOCAL-auth (synced: false)
When:  Run `featctl manifest sync`
Then:  
  - Server has new feature with FT-000001 format
  - Manifest updated: FT-LOCAL-auth removed, FT-000001 added
  - Entry has synced: true, alias: "FT-LOCAL-auth"
```

### C3: TestLint_Offline_Valid

```
Given: 
  - Manifest with FT-LOCAL-dashboard (synced: false)
  - YAML file with feature_id: FT-LOCAL-dashboard
When:  Run `featctl lint --offline <file>`
Then:  Exit 0, feature validated against manifest
```

### C4: TestLint_Offline_NotFound

```
Given:
  - Manifest without FT-LOCAL-unknown
  - YAML file with feature_id: FT-LOCAL-unknown  
When:  Run `featctl lint --offline <file>`
Then:  Exit 1, error "feature not found in manifest"
```

## Makefile Additions

```makefile
.PHONY: test-integration
test-integration: docker-build
	go test -v -count=1 -tags=integration ./test/integration/...

.PHONY: test-e2e
test-e2e: docker-build build
	go test -v -count=1 -tags=e2e ./test/e2e/...

.PHONY: test-all
test-all: test test-integration test-e2e
```

## Key Implementation Notes

1. **Build tags**: Use `//go:build integration` to separate from unit tests
2. **Parallel safety**: Each test gets isolated container + temp directory
3. **Timeout**: 60s per test (container startup ~10s)
4. **Skip condition**: Skip if Docker not available
5. **CI considerations**: Ensure Docker-in-Docker works or use CI service containers

## Estimated Effort

| Phase | Tasks | Estimate |
|-------|-------|----------|
| A (Infrastructure) | 5 | 1-2 hours |
| B (Integration) | 10 | 2-3 hours |
| C (E2E) | 7 | 1-2 hours |
| **Total** | **22** | **4-7 hours** |

## Acceptance Criteria

1. `make test-integration` passes with real containerized server
2. `make test-e2e` passes with CLI against containerized server
3. All PRD Phase 2 & 3 test items marked complete
4. No network mocking - real mTLS connections
5. Tests reproducible in CI environment
