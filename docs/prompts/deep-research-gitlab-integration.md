# Deep Research Prompt: GitLab Catalog Integration for Feature Atlas CLI

> **Usage**: Copy the PROMPT section (between `---BEGIN PROMPT---` and `---END PROMPT---`) into GPT-5.2 Pro Deep Research mode.
> **Expected output**: Structured technical specification (~500 lines) answering all architecture questions.

---BEGIN PROMPT---

<role>
You are a senior software architect specializing in Go CLI tooling, GitLab API integration, and distributed systems. Provide authoritative, production-ready recommendations with complete code examples.
</role>

<context>
# Existing System: Feature Atlas Service

A Go 1.25.5 CLI tool (featctl) for managing a feature catalog.

## Architecture Overview

| Component | Technology | Purpose |
|-----------|------------|---------|
| Backend | Custom mTLS server (feature-atlasd) | Feature storage (in-memory) |
| CLI | spf13/cobra v1.10.2 | Command interface |
| TUI | charmbracelet/bubbletea v1.3.10 | Interactive browsing |
| Forms | charmbracelet/huh v0.8.0 | Feature creation forms |
| Manifest | .feature-atlas.yaml | Local offline tracking |
| Cache | .fas/ directory (gitignored) | Validation hints |

## Data Models (CRITICAL - two distinct Feature types exist)

API Client types (internal/apiclient/client.go):

    type Feature struct {
        ID        string    // FT-NNNNNN (server-assigned)
        Name      string
        Summary   string
        Owner     string
        Tags      []string
        CreatedAt time.Time
    }

    type SuggestItem struct {
        ID      string
        Name    string
        Summary string
    }

    type CreateFeatureRequest struct {
        Name    string
        Summary string
        Owner   string   // optional
        Tags    []string // optional
    }

    type ClientInfo struct {
        Name        string  // Certificate CN
        Role        string  // "admin" or "user"
        Fingerprint string  // Certificate fingerprint
        Subject     string  // Full certificate subject
    }

Manifest types (internal/manifest/manifest.go):

    type Entry struct {
        Name     string
        Summary  string
        Owner    string   // optional
        Tags     []string // optional
        Synced   bool
        SyncedAt string   // RFC3339
        Alias    string   // Original local ID after sync
    }

    type Feature struct {
        ID       string
        Name     string
        Summary  string
        Owner    string
        Tags     []string
        IsSynced bool
    }

## Current Client Interface (internal/apiclient/client.go)

    type Client struct {
        BaseURL string
        HTTP    *http.Client
    }

    // All methods take context.Context as first parameter

    // Read operations (all users)
    func (c *Client) Me(ctx context.Context) (*ClientInfo, error)
    func (c *Client) Suggest(ctx context.Context, query string, limit int) ([]SuggestItem, error)
    func (c *Client) Search(ctx context.Context, query string, limit int) ([]Feature, error)
    func (c *Client) GetFeature(ctx context.Context, id string) (*Feature, error)
    func (c *Client) FeatureExists(ctx context.Context, id string) (bool, error)

    // Write operations (admin only)
    func (c *Client) CreateFeature(ctx context.Context, req CreateFeatureRequest) (*Feature, error)

## Existing Error Patterns

    // apiclient package
    var ErrFeatureNotFound = errors.New("feature not found")

    // manifest package
    var ErrManifestNotFound = errors.New("manifest not found")
    var ErrInvalidID        = errors.New("invalid feature ID format")
    var ErrIDExists         = errors.New("feature ID already exists in manifest")
    var ErrLockTimeout      = errors.New("manifest locked by another process")
    var ErrInvalidYAML      = errors.New("invalid YAML")
    var ErrEmptyName        = errors.New("feature name cannot be empty")
    var ErrEmptySummary     = errors.New("feature summary cannot be empty")

## Current CLI Flags (cmd/featctl/main.go)

    --server    string   Feature Atlas server URL (default "https://localhost:8443")
    --ca        string   CA certificate file (default "certs/ca.crt")
    --cert      string   Client certificate file (default "certs/alice.crt")
    --key       string   Client private key file (default "certs/alice.key")
    --manifest  string   Custom manifest path
    --sync      bool     Sync after adding features

## ID Formats

    Server ID:  FT-NNNNNN           (regex: ^FT-[0-9]{6}$)
    Local ID:   FT-LOCAL-[suffix]   (regex: ^FT-LOCAL-[a-z0-9-]{1,64}$)

## TUI Data Flow

    User types → Suggest(query, 10) → []SuggestItem displayed  // maxSuggestions=10
    User selects → items added to manifest
    User confirms → CreateFeature() if syncing

## Design Principles (MUST follow)

- DRY: No code duplication between backends
- SST: Single source of truth for feature data
- Interface-first: Abstract backends behind common interface
</context>

<objective>
Design a dual-mode architecture for featctl supporting:

1. Atlas Mode (existing): mTLS connection to custom server
2. GitLab Mode (new): Git-based catalog with MR workflow for writes

Mode selection via configuration file or CLI flags. Both modes MUST implement the same interface to avoid code duplication in TUI/commands.
</objective>

<research_questions>

## Q1: Git Catalog Structure

Design the file structure for the GitLab-hosted feature catalog repository.

Requirements:
- Support 1000+ features without performance issues
- Minimize merge conflicts when multiple MRs modify catalog
- Enable git blame per feature
- Support partial clones for large catalogs

Evaluate and recommend ONE of:
- Single YAML file (features.yaml)
- Directory per feature (features/FT-000001.yaml)
- Sharded files (features/00-09.yaml, features/10-19.yaml)

Provide: exact file structure, example feature file content, .gitattributes if needed.

## Q2: GitLab Go Client

Confirm the correct library and identify required APIs.

Library: gitlab.com/gitlab-org/api/client-go (v1.16.0+)

List ALL required API operations with method signatures:
- Repository files (read catalog)
- Branches (create feature branches)
- Commits (add/modify feature files)
- Merge Requests (create, check status)
- Users/Groups (for owner validation, optional)

Provide: minimal working code example showing MR creation flow.

## Q3: Authentication Architecture

### Q3a: Interactive Users (CLI on developer machines)

Compare for GitLab CLI authentication:
- OAuth2 Authorization Code with PKCE
- Device Authorization Grant (GitLab 17.1+)

Recommend ONE with: UX flow description, token refresh strategy, logout handling.

### Q3b: Machine-to-Machine (CI/CD pipelines)

Compare:
- Personal Access Token (PAT)
- Project Access Token
- Group Access Token
- CI Job Token

Recommend by scenario:
- Read-only catalog access
- Write access (create MRs)
- Cross-project access

Specify: minimum required scopes for each.

### Q3c: Credential Storage

Compare for storing GitLab tokens:
- zalando/go-keyring (system keyring)
- Config file (~/.config/featctl/credentials.yaml)
- Environment variables only

Recommend: primary method + fallback for headless/container environments.

## Q4: Backend Interface Design

Design a Go interface that both Atlas and GitLab backends implement.

Requirements:
- TUI must work identically with both backends
- Commands must not know which backend is active
- Error types must be backend-agnostic
- Support for both SuggestItem (autocomplete) and Feature (full data)

Provide:
- Complete interface definition with all methods
- Backend-agnostic error types
- Factory function signature for backend selection
- Example showing TUI using the interface

## Q5: Merge Request Workflow

Design the complete MR flow for adding a feature via GitLab mode.

Specify:
- Branch naming: pattern and uniqueness strategy
- Commit message: format following Conventional Commits
- MR title: format
- MR description: template with required sections
- Labels: auto-applied labels
- Assignee/Reviewer: strategy (CODEOWNERS, config, none)

Handle edge cases:
- Concurrent MRs for same feature name
- MR creation fails mid-flow
- User lacks write access

Provide: complete code flow pseudocode.

## Q6: Configuration Schema

Design the configuration file for dual-mode operation.

Requirements:
- Support switching between Atlas and GitLab modes
- Support multiple GitLab instances (gitlab.com + self-hosted)
- Support per-directory overrides (like .gitconfig)
- Environment variable overrides for all settings

Provide:
- Complete YAML schema with comments
- File locations (global, project)
- Environment variable naming convention
- Example configs for: Atlas mode, GitLab.com, self-hosted GitLab

## Q7: Sync Strategy (GitLab Mode)

Design sync behavior for GitLab mode. Note: GitLab catalog is read via API, written via MR.

Scenarios to handle:
- featctl tui: list features from GitLab catalog
- featctl manifest sync: push local features to GitLab via MR
- Local manifest has feature not in GitLab (create MR)
- GitLab has feature not in local manifest (pull to manifest)
- Same feature ID exists in both with different data (conflict)

Specify:
- Default conflict resolution policy
- User override options (--force-local, --force-remote)
- Sync status display format

## Q8: Update and Delete Operations

Design how feature updates and deletions work via MR workflow.

For updates:
- How does user modify existing feature?
- Single MR per change or batch changes?
- How to handle concurrent updates?

For deletions:
- Soft delete (mark deprecated) vs hard delete (remove file)?
- Approval requirements for deletion?

## Q9: Rate Limiting and Resilience

Design handling for GitLab API constraints.

Address:
- Rate limit handling (429 responses)
- Retry strategy with backoff
- Offline mode behavior
- Caching strategy for read operations
- Timeout configuration

</research_questions>

<output_format>
Structure response as:

## Executive Summary
2-3 sentences on recommended overall approach.

## Q1: Git Catalog Structure
### Recommendation
[Direct answer]
### Rationale
[Why this choice over alternatives]
### Implementation
[File structure, example content]

## Q2: GitLab Go Client
### Recommendation
[Library + version]
### Required APIs
[Table: Operation | Method | Purpose]
### Code Example
[Working Go code for MR creation]

## Q3: Authentication Architecture
### Q3a: Interactive Users
[Recommendation + flow]
### Q3b: Machine-to-Machine
[Recommendation table by scenario]
### Q3c: Credential Storage
[Recommendation + fallback]

## Q4: Backend Interface Design
### Interface Definition
[Complete Go interface code]
### Error Types
[Backend-agnostic errors]
### Factory Pattern
[Backend selection code]

## Q5: Merge Request Workflow
### Specifications
[Branch, commit, MR formats]
### Edge Case Handling
[Each case with solution]
### Flow Pseudocode
[Complete flow]

## Q6: Configuration Schema
### Schema
[Complete YAML with comments]
### File Locations
[Precedence order]
### Environment Variables
[Mapping table]

## Q7: Sync Strategy
### Behavior Matrix
[Table: Scenario | Action | User Feedback]
### Conflict Resolution
[Policy + overrides]

## Q8: Update and Delete Operations
### Update Flow
[Process description]
### Delete Flow
[Process description]

## Q9: Rate Limiting and Resilience
### Strategy
[Retry, backoff, caching]
### Configuration
[Timeout, retry settings]

## Implementation Roadmap
Ordered phases:
1. [Phase name]: [Deliverables] - [Dependencies]
2. ...

## Risk Assessment
| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
</output_format>

<constraints>
- Target: GitLab.com AND self-hosted instances (GitLab 17.1+ for Device Auth Grant)
- Go version: 1.25.5
- Library: gitlab.com/gitlab-org/api/client-go v1.16.0+
- Credential storage: zalando/go-keyring v0.2.6+
- Must work offline with local manifest when GitLab unreachable
- Must not break existing Atlas mode functionality
- All recommendations must include concrete code or configuration examples
</constraints>

---END PROMPT---

## Expected Clarification Questions

The model may ask clarifying questions before responding. Use these pre-approved answers:

| Question | Answer |
|----------|--------|
| "All questions at once or step by step?" | **All questions (Q1-Q9) in a single comprehensive response.** The questions are interconnected: Q4 depends on Q1-Q3, Q5 depends on Q1, Q7 depends on Q4+Q5, and the Implementation Roadmap requires all answers to sequence dependencies. |
| "Which GitLab instance?" | **Both GitLab.com AND self-hosted.** Design must work with both. |
| "Specific catalog repo path?" | **Configurable via config file.** No hardcoded path—use placeholder `<project-path>` in examples. |
| "Preferred auth method?" | **Let the research determine the best option.** Compare alternatives as specified in Q3. |
| "Any existing GitLab integration?" | **No.** This is a greenfield addition to the existing Atlas-only architecture. |

## Validation Checklist

Before submitting to GPT-5.2 Pro, verify:

- [ ] Prompt copied between ---BEGIN PROMPT--- and ---END PROMPT--- markers only
- [ ] No nested code blocks (all code uses 4-space indent, not triple backticks)
- [ ] All XML-style tags are properly closed

## Post-Research Actions

1. Create `docs/todo/SWD-gitlab-catalog-integration.md` from response
2. Extract acceptance criteria from each Q answer
3. Define test strategy per component
4. Estimate effort per implementation phase
