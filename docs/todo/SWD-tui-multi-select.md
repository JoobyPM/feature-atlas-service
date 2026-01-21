# TUI Multi-Select & Manifest Integration

## Goal

Enhance `featctl tui` to support multi-feature selection with manifest integration and optional auto-sync.

## Current Behavior

- TUI shows feature list with autocomplete search
- Single selection exits immediately
- No manifest integration
- Must run separate `manifest add` commands

## Target Behavior

- Multi-select mode with Space to toggle, Enter to confirm
- Visual indicators for selected features and manifest status
- Confirmation dialog before manifest changes
- Optional `--sync` flag for immediate server sync

## Requirements

### R1: Multi-Select Mode

| Key | Action |
|-----|--------|
| `↑/↓` or `j/k` | Navigate list |
| `Space` | Toggle selection on current item |
| `Enter` | Confirm selection and proceed |
| `a` | Select all visible |
| `n` | Deselect all |
| `Esc` or `q` | Cancel and exit |

### R2: Visual Indicators

```
┌─ Feature Search ─────────────────────────────────────┐
│ > auth                                               │
├──────────────────────────────────────────────────────┤
│ [✓] FT-000001  Authentication     [in manifest]      │
│ [ ] FT-000002  Authorization      [on server]        │
│ [✓] FT-000003  OAuth Provider     [on server]        │
│ [ ] FT-LOCAL-x Local Feature      [local only]       │
├──────────────────────────────────────────────────────┤
│ Selected: 2 │ Space: toggle │ Enter: add │ q: quit  │
└──────────────────────────────────────────────────────┘
```

### R3: Confirmation Dialog

Before modifying manifest, show:

```
Add 2 feature(s) to manifest?

  • FT-000001 - Authentication
  • FT-000003 - OAuth Provider

[Y]es  [N]o  [S]ync after adding
```

### R4: Auto-Sync Flag

```bash
featctl tui --sync          # Sync immediately after adding
featctl tui --manifest path # Custom manifest location
```

## Implementation

### Phase 1: Multi-Select Model

**File**: `internal/tui/tui.go`

- Add `selected map[string]bool` to Model
- Add `manifestFeatures map[string]bool` for status display
- Implement toggle logic in `Update()`
- Update `View()` for selection indicators

### Phase 2: Confirmation View

**File**: `internal/tui/tui.go`

- Add `confirmMode bool` state
- Add `syncAfterAdd bool` option
- Render confirmation dialog in `View()`
- Handle Y/N/S keys in confirmation mode

### Phase 3: Manifest Integration

**File**: `cmd/featctl/main.go`

- Load manifest at TUI start (if exists)
- Pass manifest state to TUI model
- On confirm: call `manifest.AddFeature()` for each
- If sync requested: call server API

### Phase 4: CLI Flags

**File**: `cmd/featctl/main.go`

- Add `--sync` flag to `tuiCmd`
- Add `--manifest` flag (reuse from other commands)
- Pass flags to TUI initialization

## Acceptance Criteria

1. [ ] Space toggles feature selection
2. [ ] Enter opens confirmation dialog
3. [ ] `[in manifest]` / `[on server]` status shown
4. [ ] Confirmation shows feature list before changes
5. [ ] `--sync` flag syncs after manifest update
6. [ ] No regressions in single-select behavior (Enter on unselected = select + exit)

## Testing

| Test | Scenario |
|------|----------|
| Unit | Model selection state management |
| Unit | Confirmation dialog rendering |
| E2E | Multi-select → confirm → manifest updated |
| E2E | Multi-select → sync → features on server |

## Out of Scope

- Batch delete from manifest
- Edit feature metadata in TUI
- Server-side multi-select operations
