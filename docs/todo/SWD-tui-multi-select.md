# TUI Multi-Select & Manifest Integration

## Status: ✅ COMPLETED

## Goal

Enhance `featctl tui` to support multi-feature selection with manifest integration, optional auto-sync, and feature creation.

## What Was Implemented

### Multi-Select Mode (Phase 1-4)
- Multi-select with Space to toggle, Enter to confirm
- Visual indicators for selected features and manifest status
- Confirmation dialog before manifest changes
- `--sync` flag for immediate server sync
- `--manifest` flag for custom manifest location

### Feature Creation Form (Phase 5) - Added Jan 2026
- Press `n` to open feature creation form (uses `huh` library)
- Form validates name uniqueness (cache + server)
- Creates feature on server → adds to local manifest
- Local cache (`.fas/`) for fast validation hints

## Keybindings

| Key | Action |
|-----|--------|
| `↑/↓` | Navigate list |
| `Space` | Toggle selection on current item |
| `n` | Open feature creation form (requires admin cert) |
| `Enter` | Confirm selection and proceed |
| `Ctrl+A` | Select all visible |
| `Ctrl+N` | Deselect all |
| `Esc` | Clear search or quit |
| Any letter | Type in search (including j, k, q) |

## Visual Layout

### Search View
```
🔍 Feature Atlas

> auth

  [✓] FT-000001  Authentication     [in manifest]
  [ ] FT-000002  Authorization      [on server]
  [✓] FT-000003  OAuth Provider     [on server]
  [ ] FT-LOCAL-x Local Feature      [local only]

Selected: 2

↑/↓: navigate • Space: toggle • n: new • Ctrl+A: all • Ctrl+N: none • Enter: confirm • Esc: quit
```

### Confirmation Dialog
```
Add 2 feature(s) to manifest?

  • FT-000001 - Authentication
  • FT-000003 - OAuth Provider

[Y]es  [N]o  [S]ync after adding
```

### Feature Creation Form (press `n`)
```
Create New Feature

  Name *
  ┌────────────────────────────────────────────────────┐
  │ Authentication Service                             │
  └────────────────────────────────────────────────────┘

  Summary *
  ┌────────────────────────────────────────────────────┐
  │ Handles user authentication via OAuth2             │
  └────────────────────────────────────────────────────┘

  Owner (optional)
  ┌────────────────────────────────────────────────────┐
  │ platform-team                                      │
  └────────────────────────────────────────────────────┘

  Tags (comma-separated, optional)
  ┌────────────────────────────────────────────────────┐
  │ auth, security                                     │
  └────────────────────────────────────────────────────┘

Tab: next • Shift+Tab: prev • Enter: submit • Esc: cancel
```

## CLI Flags

```bash
featctl tui                           # Normal mode (alice cert)
featctl tui --sync                    # Auto-sync after adding
featctl tui --manifest path           # Custom manifest location
featctl tui --cert certs/admin.crt --key certs/admin.key  # Admin mode (required for 'n' create)
```

## Files Changed

| File | Changes |
|------|---------|
| `internal/tui/tui.go` | Multi-select model, confirmation view, 'n' key handler, cache/manifest integration |
| `internal/tui/form.go` | **NEW** - Feature creation form using `huh` library |
| `internal/tui/form_test.go` | **NEW** - Form unit tests (34 tests) |
| `internal/tui/tui_test.go` | Added tests for 'n' key, form delegation, view rendering |
| `internal/cache/cache.go` | **NEW** - Local cache for validation hints (`.fas/` directory) |
| `internal/cache/cache_test.go` | **NEW** - Cache unit tests (14 tests) |
| `internal/manifest/manifest.go` | Added `Feature` struct and `AddSyncedFeature()` method |
| `internal/manifest/manifest_test.go` | Added `TestAddSyncedFeature` (6 test cases) |
| `cmd/featctl/main.go` | TUI options with cache/manifest, updated help text |
| `go.mod` | Added `github.com/charmbracelet/huh` dependency |

## Acceptance Criteria

1. [x] Space toggles feature selection
2. [x] Enter opens confirmation dialog
3. [x] `[in manifest]` / `[on server]` / `[local only]` status shown
4. [x] Confirmation shows feature list before changes
5. [x] `--sync` flag syncs after manifest update
6. [x] No regressions in single-select behavior
7. [x] `n` key opens feature creation form
8. [x] Form validates name uniqueness (cache + server)
9. [x] Created feature saved to server and manifest
10. [x] Local cache (`.fas/`) refreshes when stale

## Test Coverage

| Component | Tests | Coverage |
|-----------|-------|----------|
| `internal/tui` (form) | 34 | Form states, validation, view rendering |
| `internal/tui` (model) | 26 | Selection, navigation, 'n' key, delegation |
| `internal/cache` | 14 | Load/save, staleness, thread safety |
| `internal/manifest` | 6 | AddSyncedFeature validation |
| **Total** | **80+** | All critical paths |

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                          TUI Model                              │
│  ┌──────────────┐    ┌──────────────┐    ┌───────────────┐      │
│  │StateSearching│◄──►│ StateConfirm │    │  StateCreating│      │
│  │              │    │              │    │  (FormModel)  │      │
│  └──────────────┘    └──────────────┘    └───────────────┘      │
│         │                   │                    │              │
│         ▼                   ▼                    ▼              │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │                    API Client (mTLS)                    │    │
│  │  - Search/Suggest (all users)                           │    │
│  │  - CreateFeature (admin only)                           │    │
│  └─────────────────────────────────────────────────────────┘    │
│         │                                        │              │
│         ▼                                        ▼              │
│  ┌──────────────┐                        ┌──────────────┐       │
│  │   Manifest   │ ◄─── SST (committed) ──│    Cache     │       │
│  │ .feature-    │                        │    .fas/     │       │
│  │  atlas.yaml  │                        │ (gitignored) │       │
│  └──────────────┘                        └──────────────┘       │
└─────────────────────────────────────────────────────────────────┘
```

## Out of Scope

- Batch delete from manifest
- Edit feature metadata in TUI
- Server-side multi-select operations
