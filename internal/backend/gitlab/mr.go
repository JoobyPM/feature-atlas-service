package gitlab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gitlab.com/gitlab-org/api/client-go"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

// MR operation types.
const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"
)

// MRConfig holds merge request configuration.
type MRConfig struct {
	Labels             []string
	RemoveSourceBranch bool
	DefaultAssignee    string
}

// slugPattern matches characters to keep in slugs.
var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a string to a URL-safe slug.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = slugPattern.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 30 {
		s = s[:30]
		s = strings.TrimSuffix(s, "-") // Trim trailing hyphen after truncation
	}
	return s
}

// randomSuffix generates a random 4-character hex string.
func randomSuffix() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based suffix
		return fmt.Sprintf("%04x", time.Now().UnixNano()&0xFFFF)
	}
	return hex.EncodeToString(b)
}

// BranchName generates a branch name for the given operation.
// All branch names include a random suffix to prevent collisions when
// multiple updates to the same feature are in progress.
func BranchName(op string, feature *backend.Feature) string {
	slug := slugify(feature.Name)
	if slug == "" {
		slug = "feature"
	}

	switch op {
	case OpCreate:
		return fmt.Sprintf("feature/add-%s-%s", slug, randomSuffix())
	case OpUpdate:
		return fmt.Sprintf("feature/update-%s-%s-%s", feature.ID, slug, randomSuffix())
	case OpDelete:
		return fmt.Sprintf("feature/delete-%s-%s", feature.ID, randomSuffix())
	default:
		return fmt.Sprintf("feature/%s-%s-%s", op, slug, randomSuffix())
	}
}

// CommitMessage generates a commit message for the given operation.
func CommitMessage(op string, feature *backend.Feature) string {
	switch op {
	case OpCreate:
		return fmt.Sprintf("feat: add feature %s (ID: %s)", feature.Name, feature.ID)
	case OpUpdate:
		return fmt.Sprintf("chore: update feature %s (ID: %s)", feature.Name, feature.ID)
	case OpDelete:
		return fmt.Sprintf("chore: delete feature %s (ID: %s)", feature.Name, feature.ID)
	default:
		return fmt.Sprintf("chore: %s feature %s (ID: %s)", op, feature.Name, feature.ID)
	}
}

// MRTitle generates a merge request title for the given operation.
func MRTitle(op string, feature *backend.Feature) string {
	switch op {
	case OpCreate:
		return fmt.Sprintf("Add feature: %s (ID: %s)", feature.Name, feature.ID)
	case OpUpdate:
		return fmt.Sprintf("Update feature: %s (ID: %s)", feature.Name, feature.ID)
	case OpDelete:
		return fmt.Sprintf("Delete feature: %s (ID: %s)", feature.Name, feature.ID)
	default:
		// Capitalize first letter manually to avoid deprecated strings.Title
		opTitle := op
		if len(op) > 0 {
			opTitle = strings.ToUpper(op[:1]) + op[1:]
		}
		return fmt.Sprintf("%s feature: %s (ID: %s)", opTitle, feature.Name, feature.ID)
	}
}

// MRDescription generates a merge request description for the given operation.
func MRDescription(op string, feature *backend.Feature) string {
	var sb strings.Builder

	switch op {
	case OpCreate:
		sb.WriteString("## Feature Proposal\n\n")
	case OpUpdate:
		sb.WriteString("## Feature Update\n\n")
	case OpDelete:
		sb.WriteString("## Feature Deletion\n\n")
	default:
		sb.WriteString("## Feature Change\n\n")
	}

	sb.WriteString(fmt.Sprintf("**Name:** %s\n", feature.Name))
	sb.WriteString(fmt.Sprintf("**ID:** %s\n", feature.ID))

	if feature.Summary != "" {
		sb.WriteString(fmt.Sprintf("**Summary:** %s\n", feature.Summary))
	}
	if feature.Owner != "" {
		sb.WriteString(fmt.Sprintf("**Owner:** %s\n", feature.Owner))
	}
	if len(feature.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("**Tags:** %s\n", strings.Join(feature.Tags, ", ")))
	}

	sb.WriteString("\n## Checklist\n\n")
	switch op {
	case OpCreate:
		sb.WriteString("- [x] YAML file added under `features/`\n")
		sb.WriteString("- [ ] (Reviewer) Check for duplicates\n")
		sb.WriteString("- [ ] (Reviewer) Validate owner exists\n")
	case OpUpdate:
		sb.WriteString("- [x] YAML file updated under `features/`\n")
		sb.WriteString("- [ ] (Reviewer) Verify changes are correct\n")
	case OpDelete:
		sb.WriteString("- [x] YAML file removed from `features/`\n")
		sb.WriteString("- [ ] (Reviewer) Confirm deletion is intended\n")
		sb.WriteString("- [ ] (Reviewer) Check for dependencies\n")
	}

	return sb.String()
}

// createBranch creates a new branch from the main branch.
func (b *Backend) createBranch(ctx context.Context, branchName string) error {
	opts := &gitlab.CreateBranchOptions{
		Branch: gitlab.Ptr(branchName),
		Ref:    gitlab.Ptr(b.mainBranch),
	}

	_, resp, err := b.client.Branches.CreateBranch(b.project, opts, gitlab.WithContext(ctx))
	if err != nil {
		return mapError(resp, err)
	}
	return nil
}

// deleteBranch deletes a branch.
func (b *Backend) deleteBranch(ctx context.Context, branchName string) error {
	resp, err := b.client.Branches.DeleteBranch(b.project, branchName, gitlab.WithContext(ctx))
	if err != nil {
		return mapError(resp, err)
	}
	return nil
}

// commitFile creates or updates a file in a branch.
func (b *Backend) commitFile(ctx context.Context, branch, filePath, content, commitMessage string) error {
	opts := &gitlab.CreateFileOptions{
		Branch:        gitlab.Ptr(branch),
		Content:       gitlab.Ptr(content),
		CommitMessage: gitlab.Ptr(commitMessage),
	}

	_, resp, err := b.client.RepositoryFiles.CreateFile(b.project, filePath, opts, gitlab.WithContext(ctx))
	if err != nil {
		return mapError(resp, err)
	}
	return nil
}

// updateFile updates a file in a branch.
func (b *Backend) updateFile(ctx context.Context, branch, filePath, content, commitMessage string) error {
	opts := &gitlab.UpdateFileOptions{
		Branch:        gitlab.Ptr(branch),
		Content:       gitlab.Ptr(content),
		CommitMessage: gitlab.Ptr(commitMessage),
	}

	_, resp, err := b.client.RepositoryFiles.UpdateFile(b.project, filePath, opts, gitlab.WithContext(ctx))
	if err != nil {
		return mapError(resp, err)
	}
	return nil
}

// deleteFile deletes a file in a branch.
func (b *Backend) deleteFile(ctx context.Context, branch, filePath, commitMessage string) error {
	opts := &gitlab.DeleteFileOptions{
		Branch:        gitlab.Ptr(branch),
		CommitMessage: gitlab.Ptr(commitMessage),
	}

	resp, err := b.client.RepositoryFiles.DeleteFile(b.project, filePath, opts, gitlab.WithContext(ctx))
	if err != nil {
		return mapError(resp, err)
	}
	return nil
}

// MRInfo contains information about a created merge request.
type MRInfo struct {
	IID    int    // GitLab merge request internal ID
	URL    string // Full URL to the merge request
	Branch string // Source branch name
}

// createMergeRequest creates a merge request and returns its info.
func (b *Backend) createMergeRequest(ctx context.Context, branch, title, description string, cfg MRConfig) (*MRInfo, error) {
	opts := &gitlab.CreateMergeRequestOptions{
		SourceBranch:       gitlab.Ptr(branch),
		TargetBranch:       gitlab.Ptr(b.mainBranch),
		Title:              gitlab.Ptr(title),
		Description:        gitlab.Ptr(description),
		RemoveSourceBranch: gitlab.Ptr(cfg.RemoveSourceBranch),
	}

	if len(cfg.Labels) > 0 {
		opts.Labels = gitlab.Ptr(gitlab.LabelOptions(cfg.Labels))
	}

	// Try to resolve assignee
	if cfg.DefaultAssignee != "" {
		users, _, lookupErr := b.client.Users.ListUsers(&gitlab.ListUsersOptions{
			Username: gitlab.Ptr(cfg.DefaultAssignee),
		}, gitlab.WithContext(ctx))
		if lookupErr == nil && len(users) > 0 {
			opts.AssigneeID = gitlab.Ptr(users[0].ID)
		}
	}

	mr, resp, err := b.client.MergeRequests.CreateMergeRequest(b.project, opts, gitlab.WithContext(ctx))
	if err != nil {
		return nil, mapError(resp, err)
	}

	return &MRInfo{
		IID:    int(mr.IID),
		URL:    mr.WebURL,
		Branch: branch,
	}, nil
}

// getExistingFeatureIDs returns all existing feature IDs from the main branch.
func (b *Backend) getExistingFeatureIDs(ctx context.Context) ([]string, error) {
	features, err := b.loadFeatures(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]string, len(features))
	for i, f := range features {
		ids[i] = f.ID
	}
	return ids, nil
}

// assignNextID determines the next available feature ID.
// Returns the feature with the assigned ID.
func (b *Backend) assignNextID(ctx context.Context, feature backend.Feature) (*backend.Feature, error) {
	existingIDs, err := b.getExistingFeatureIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("get existing IDs: %w", err)
	}

	feature.ID = NextFeatureID(existingIDs)
	return &feature, nil
}
