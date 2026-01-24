# GitLab Feature Catalog Workflow

This document describes the workflow for managing features using the GitLab-based feature catalog.

## Overview

The feature catalog uses a Git repository as the source of truth. Features are stored as YAML files in the `features/` directory and managed through merge requests (MRs).

```
Repository
└── features/
    ├── FT-000001.yaml
    ├── FT-000002.yaml
    └── ...
```

## Workflow Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                     Developer Workflow                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   1. Create Feature          2. Review & Merge                  │
│   ┌──────────────┐           ┌──────────────┐                   │
│   │ featctl tui  │──────────▶│ GitLab MR    │                   │
│   │ Press 'n'    │  Creates  │ Review       │                   │
│   └──────────────┘  MR       └──────┬───────┘                   │
│         │                           │                           │
│         ▼                           ▼                           │
│   ┌──────────────┐           ┌──────────────┐                   │
│   │ Local Cache  │           │ features/    │                   │
│   │ [PENDING]    │           │ (merged)     │                   │
│   └──────────────┘           └──────────────┘                   │
│                                                                 │
│   3. CI Validation                                              │
│   ┌──────────────────────────────────────────────────────────┐  │
│   │ featctl lint --strict                                    │  │
│   │   ✓ All features in manifest exist in merged catalog     │  │
│   │   ✗ FAIL if any feature is pending (not merged)          │  │
│   └──────────────────────────────────────────────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Feature File Format

Each feature is stored as a YAML file in `features/FT-NNNNNN.yaml`:

```yaml
id: FT-000001
name: User Authentication
summary: OAuth2 and JWT-based authentication system
owner: platform-team
domain: security           # Required: business domain
component: auth-service    # Optional: technical component
tags:
  - authentication
  - security
  - oauth2
created_at: "2026-01-23T12:00:00Z"
updated_at: "2026-01-23T12:00:00Z"
```

### Required Fields

| Field | Description |
|-------|-------------|
| `id` | Unique identifier (FT-NNNNNN format) |
| `name` | Human-readable feature name |
| `summary` | Brief description |
| `domain` | Business domain (e.g., security, payments, user-management) |

### Optional Fields

| Field | Description |
|-------|-------------|
| `owner` | Team or person responsible |
| `component` | Technical component (e.g., auth-service, api-gateway) |
| `tags` | Free-form categorization tags |

## Local Development Flow

### 1. Create a Feature

```bash
# Interactive mode
featctl tui
# Press 'n' to create new feature
# Fill in: Name, Summary, Owner, Domain, Tags

# Or command line
featctl feature create --name "My Feature" --summary "Description" --domain "my-domain"
```

This creates a merge request on GitLab. The feature appears locally with `[PENDING]` status.

### 2. View Pending Features

```bash
# Search includes pending features
featctl search ""
# Output:
# FT-000001  User Authentication
#     OAuth2 and JWT-based authentication system
#
# FT-000002  [PENDING] My New Feature
#     Description of my new feature

# List pending MRs
featctl pending
```

### 3. Local Manifest

Pending features can be used in your local manifest during development:

```bash
# Add to manifest (with warning for pending)
featctl manifest add FT-000002
# ⚠ Warning: FT-000002 is pending (MR not merged)

featctl manifest list
```

### 4. Merge the MR

1. Go to the MR URL shown when creating the feature
2. Review the changes
3. Merge the MR

The local cache auto-updates on next `search` or `tui` command.

## CI/CD Integration

### Strict Mode

In CI pipelines, use `--strict` flag to fail if any pending features are referenced:

```bash
featctl lint --strict
```

This ensures only approved (merged) features are used in production.

### GitLab CI/CD Setup

```yaml
# .gitlab-ci.yml
feature-lint:
  stage: validate
  image: golang:1.25
  variables:
    FEATCTL_GITLAB_TOKEN: $CI_JOB_TOKEN
  script:
    - go install github.com/JoobyPM/feature-atlas-service/cmd/featctl@latest
    - featctl --mode gitlab --gitlab-project $CI_PROJECT_PATH lint --strict
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
```

### GitHub Actions Setup

```yaml
# .github/workflows/feature-lint.yml
name: Feature Lint
on: [pull_request]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      
      - name: Install featctl
        run: go install github.com/JoobyPM/feature-atlas-service/cmd/featctl@latest
      
      - name: Lint features
        env:
          FEATCTL_GITLAB_TOKEN: ${{ secrets.GITLAB_TOKEN }}
        run: |
          featctl --mode gitlab \
            --gitlab-instance https://gitlab.com \
            --gitlab-project your-org/your-repo \
            lint --strict
```

## Feature Status Indicators

| Status | Indicator | Description |
|--------|-----------|-------------|
| Merged | (none) | Feature is approved and in the catalog |
| Pending | `[PENDING]` | MR created, awaiting review/merge |
| Closed | `[CLOSED]` | MR was closed/rejected |
| Conflict | `[CONFLICT]` | MR has merge conflicts |

## Commands Reference

```bash
# Authentication
featctl login                    # OAuth device flow
featctl logout                   # Remove credentials
featctl auth status              # Check auth status

# Browse & Search
featctl search "query"           # Search features
featctl tui                      # Interactive browser

# Feature Management
featctl feature create           # Create new feature (MR)
featctl get FT-000001            # Get feature details

# Pending MRs
featctl pending                  # List pending MRs
featctl pending refresh          # Refresh MR status

# Manifest
featctl manifest init            # Initialize local manifest
featctl manifest add FT-000001   # Add feature to manifest
featctl manifest list            # List manifest features
featctl manifest sync            # Sync with remote

# Validation
featctl lint                     # Lint manifest (local)
featctl lint --strict            # Lint manifest (CI mode)
```

## Configuration

```yaml
# ~/.config/featctl/config.yaml
mode: gitlab
gitlab:
  instance: https://gitlab.com
  project: your-org/feature-catalog
  oauth_client_id: your-client-id
  main_branch: main
```

## Troubleshooting

### "401 Unauthorized" errors

1. Check authentication: `featctl auth status`
2. Re-authenticate: `featctl login`

### "Feature not found" in CI

1. Ensure the feature MR has been merged
2. Check you're targeting the correct branch (`main`)
3. Verify project path: `--gitlab-project`

### Pending feature not updating

1. Run `featctl pending refresh` to update status
2. Check MR status on GitLab directly
