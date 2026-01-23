package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gitlab.com/gitlab-org/api/client-go"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

// MRState represents the state of a merge request.
type MRState string

// MR states.
const (
	MRStateOpen   MRState = "opened"
	MRStateMerged MRState = "merged"
	MRStateClosed MRState = "closed"
)

// unknownServerID is used when we can't determine the server-assigned ID.
const unknownServerID = "unknown"

// SyncAction represents an action to take during sync.
type SyncAction struct {
	Type        SyncActionType
	LocalID     string           // Local feature ID (FT-LOCAL-* or FT-NNNNNN)
	ServerID    string           // Server feature ID (FT-NNNNNN)
	Feature     *backend.Feature // Feature data
	PendingMR   *PendingMR       // Pending MR if any
	Description string           // Human-readable description
}

// SyncActionType represents the type of sync action.
type SyncActionType int

// Sync action types.
const (
	ActionNone         SyncActionType = iota // No action needed
	ActionCreateMR                           // Create MR for new local feature
	ActionPendingMR                          // MR is pending (waiting for merge)
	ActionMRMerged                           // MR was merged, update local
	ActionUpdateLocal                        // Pull remote changes to local
	ActionUpdateRemote                       // Push local changes via MR
	ActionConflict                           // Conflict detected
	ActionWarnNew                            // New remote feature not in local
)

// SyncResult contains the results of a sync operation.
type SyncResult struct {
	Actions  []SyncAction
	Created  int // MRs created
	Updated  int // Local entries updated
	Pending  int // MRs still pending
	Skipped  int // Skipped (conflicts, etc.)
	Warnings []string
}

// CheckMRStatus checks the status of a merge request.
func (b *Backend) CheckMRStatus(ctx context.Context, mrIID int) (MRState, *gitlab.MergeRequest, error) {
	mr, resp, err := b.client.MergeRequests.GetMergeRequest(b.project, int64(mrIID), nil, gitlab.WithContext(ctx))
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return "", nil, backend.ErrNotFound
		}
		return "", nil, mapError(resp, err)
	}

	return MRState(mr.State), mr, nil
}

// GetMergedMRFeatureID extracts the server-assigned feature ID from a merged MR.
// It parses the MR diffs to find the feature file path.
func (b *Backend) GetMergedMRFeatureID(ctx context.Context, mr *gitlab.MergeRequest) (string, error) {
	// Get the list of diffs in the MR
	opts := &gitlab.ListMergeRequestDiffsOptions{}
	diffs, resp, err := b.client.MergeRequests.ListMergeRequestDiffs(b.project, mr.IID, opts, gitlab.WithContext(ctx))
	if err != nil {
		return "", mapError(resp, err)
	}

	// Look for a feature file in the diffs
	for _, diff := range diffs {
		id := FeatureIDFromPath(diff.NewPath)
		if id != "" && !IsLocalID(id) {
			return id, nil
		}
	}

	return "", errors.New("no feature ID found in MR diffs")
}

// IsLocalID checks if an ID is a local (unsynced) feature ID.
func IsLocalID(id string) bool {
	return len(id) > 9 && id[:9] == "FT-LOCAL-"
}

// PlanSync analyzes the current state and returns planned sync actions.
// This is used for --dry-run and to show what would happen.
func (b *Backend) PlanSync(ctx context.Context, localFeatures map[string]LocalFeature, forceLocal bool) (*SyncResult, error) {
	result := &SyncResult{
		Actions: []SyncAction{},
	}

	// Load pending MRs
	pending, err := LoadPendingMRs()
	if err != nil {
		return nil, fmt.Errorf("load pending MRs: %w", err)
	}

	// Get remote features
	remoteFeatures, err := b.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list remote features: %w", err)
	}

	// Build remote feature map
	remoteMap := make(map[string]backend.Feature)
	for _, f := range remoteFeatures {
		remoteMap[f.ID] = f
	}

	// Process local features
	for localID, local := range localFeatures {
		action := b.planFeatureSync(ctx, localID, local, remoteMap, pending, forceLocal)
		if action.Type != ActionNone {
			result.Actions = append(result.Actions, action)
		}
	}

	// Check for pending MRs that might be merged
	for _, pmr := range pending.List() {
		if _, found := localFeatures[pmr.LocalID]; !found {
			// Pending MR for a feature not in local manifest - check if merged
			state, mr, checkErr := b.CheckMRStatus(ctx, pmr.MRIID)
			if checkErr == nil && state == MRStateMerged {
				serverID, getMRErr := b.GetMergedMRFeatureID(ctx, mr)
				if getMRErr != nil {
					serverID = unknownServerID
				}
				pmrCopy := pmr
				result.Actions = append(result.Actions, SyncAction{
					Type:        ActionMRMerged,
					LocalID:     pmr.LocalID,
					ServerID:    serverID,
					PendingMR:   &pmrCopy,
					Description: fmt.Sprintf("MR #%d was merged → %s", pmr.MRIID, serverID),
				})
			}
		}
	}

	// Check for new remote features not in local
	for id, remote := range remoteMap {
		found := false
		for localID := range localFeatures {
			if localID == id {
				found = true
				break
			}
		}
		if !found {
			remoteCopy := remote
			result.Actions = append(result.Actions, SyncAction{
				Type:        ActionWarnNew,
				ServerID:    id,
				Feature:     &remoteCopy,
				Description: fmt.Sprintf("New remote feature: %s (%s)", id, remote.Name),
			})
		}
	}

	return result, nil
}

// LocalFeature represents a feature from the local manifest.
type LocalFeature struct {
	Name      string
	Summary   string
	Owner     string
	Tags      []string
	Synced    bool
	SyncedAt  time.Time
	UpdatedAt time.Time // Last modified time in local manifest
}

// planFeatureSync determines what action to take for a single feature.
func (b *Backend) planFeatureSync(
	ctx context.Context,
	localID string,
	local LocalFeature,
	remoteMap map[string]backend.Feature,
	pending *PendingMRs,
	forceLocal bool,
) SyncAction {
	// Check if this is a local (unsynced) feature
	if IsLocalID(localID) {
		return b.planLocalFeatureSync(ctx, localID, local, pending)
	}

	// This is a synced feature (FT-NNNNNN) - check for updates
	return b.planSyncedFeatureSync(localID, local, remoteMap, pending, forceLocal)
}

// planLocalFeatureSync handles sync planning for local (FT-LOCAL-*) features.
func (b *Backend) planLocalFeatureSync(
	ctx context.Context,
	localID string,
	local LocalFeature,
	pending *PendingMRs,
) SyncAction {
	// Check if there's already a pending MR for this feature
	if pmr, found := pending.FindByLocalID(localID); found {
		// Check MR status
		state, mr, err := b.CheckMRStatus(ctx, pmr.MRIID)
		if err != nil {
			// Can't check MR status - treat as still pending
			return SyncAction{
				Type:        ActionPendingMR,
				LocalID:     localID,
				PendingMR:   pmr,
				Description: fmt.Sprintf("MR #%d pending (status check failed: %v)", pmr.MRIID, err),
			}
		}

		switch state {
		case MRStateMerged:
			// MR was merged - get the assigned server ID
			serverID, getMRErr := b.GetMergedMRFeatureID(ctx, mr)
			if getMRErr != nil {
				serverID = unknownServerID // Best effort
			}
			return SyncAction{
				Type:        ActionMRMerged,
				LocalID:     localID,
				ServerID:    serverID,
				PendingMR:   pmr,
				Description: fmt.Sprintf("MR #%d was merged → %s", pmr.MRIID, serverID),
			}
		case MRStateClosed:
			// MR was closed without merging - need to create new MR
			return SyncAction{
				Type:        ActionCreateMR,
				LocalID:     localID,
				Feature:     localToFeature(localID, local),
				Description: fmt.Sprintf("Previous MR #%d was closed, creating new MR", pmr.MRIID),
			}
		case MRStateOpen:
			// MR is still open
			return SyncAction{
				Type:        ActionPendingMR,
				LocalID:     localID,
				PendingMR:   pmr,
				Description: fmt.Sprintf("MR #%d is still open: %s", pmr.MRIID, pmr.MRURL),
			}
		default:
			// Unknown state - treat as pending
			return SyncAction{
				Type:        ActionPendingMR,
				LocalID:     localID,
				PendingMR:   pmr,
				Description: fmt.Sprintf("MR #%d in unknown state: %s", pmr.MRIID, state),
			}
		}
	}

	// No pending MR - need to create one
	return SyncAction{
		Type:        ActionCreateMR,
		LocalID:     localID,
		Feature:     localToFeature(localID, local),
		Description: fmt.Sprintf("Create MR for %s (%s)", localID, local.Name),
	}
}

// planSyncedFeatureSync handles sync planning for already synced (FT-NNNNNN) features.
func (b *Backend) planSyncedFeatureSync(
	localID string,
	local LocalFeature,
	remoteMap map[string]backend.Feature,
	pending *PendingMRs,
	forceLocal bool,
) SyncAction {
	remote, exists := remoteMap[localID]
	if !exists {
		// Feature was deleted remotely
		return SyncAction{
			Type:        ActionConflict,
			LocalID:     localID,
			Description: localID + " was deleted remotely",
		}
	}

	// Check if there's a pending update MR
	if pmr, found := pending.FindByServerID(localID); found {
		return SyncAction{
			Type:        ActionPendingMR,
			LocalID:     localID,
			ServerID:    localID,
			PendingMR:   pmr,
			Description: fmt.Sprintf("Update MR #%d pending: %s", pmr.MRIID, pmr.MRURL),
		}
	}

	// Compare local and remote
	localChanged := hasLocalChanges(local, remote)
	remoteChanged := hasRemoteChanges(local, remote)

	if !localChanged && !remoteChanged {
		return SyncAction{Type: ActionNone}
	}

	if localChanged && remoteChanged {
		// Conflict - both changed
		if forceLocal {
			// Push LOCAL changes to remote (overwriting remote)
			return SyncAction{
				Type:        ActionUpdateRemote,
				LocalID:     localID,
				ServerID:    localID,
				Feature:     localToFeature(localID, local),
				Description: fmt.Sprintf("Conflict: pushing local changes for %s via MR (--force-local)", localID),
			}
		}
		return SyncAction{
			Type:        ActionConflict,
			LocalID:     localID,
			ServerID:    localID,
			Description: "Conflict: both local and remote changed for " + localID,
		}
	}

	if remoteChanged {
		// Remote changed, local didn't - pull
		remoteCopy := remote
		return SyncAction{
			Type:        ActionUpdateLocal,
			LocalID:     localID,
			ServerID:    localID,
			Feature:     &remoteCopy,
			Description: "Pull remote changes for " + localID,
		}
	}

	// Local changed, remote didn't
	if forceLocal {
		return SyncAction{
			Type:        ActionUpdateRemote,
			LocalID:     localID,
			ServerID:    localID,
			Feature:     localToFeature(localID, local),
			Description: fmt.Sprintf("Push local changes for %s via MR", localID),
		}
	}

	// Default: don't push local changes unless --force-local
	return SyncAction{Type: ActionNone}
}

// hasLocalChanges checks if local has changes compared to remote.
func hasLocalChanges(local LocalFeature, remote backend.Feature) bool {
	if local.Name != remote.Name {
		return true
	}
	if local.Summary != remote.Summary {
		return true
	}
	if local.Owner != remote.Owner {
		return true
	}
	// Compare tags
	if !tagsEqual(local.Tags, remote.Tags) {
		return true
	}
	return false
}

// tagsEqual compares two tag slices for equality (order-independent).
func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	// For small slices, O(n²) is fine. For large slices, consider sorting.
	for _, tag := range a {
		found := false
		for _, other := range b {
			if tag == other {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// hasRemoteChanges checks if remote was updated after local sync.
// If local has never been synced (SyncedAt.IsZero()), we consider the remote
// as having changes that need to be pulled.
func hasRemoteChanges(local LocalFeature, remote backend.Feature) bool {
	if local.SyncedAt.IsZero() {
		return true // Never synced means remote is considered newer
	}
	return remote.UpdatedAt.After(local.SyncedAt)
}

// localToFeature converts a LocalFeature to a backend.Feature.
func localToFeature(id string, local LocalFeature) *backend.Feature {
	return &backend.Feature{
		ID:      id,
		Name:    local.Name,
		Summary: local.Summary,
		Owner:   local.Owner,
		Tags:    local.Tags,
	}
}

// ExecuteAction executes a single sync action.
func (b *Backend) ExecuteAction(ctx context.Context, action SyncAction) error {
	switch action.Type {
	case ActionCreateMR:
		return b.executeCreateMR(ctx, action)
	case ActionUpdateRemote:
		return b.executeUpdateMR(ctx, action)
	case ActionMRMerged:
		return b.executeMRMerged(action)
	case ActionNone, ActionPendingMR, ActionUpdateLocal, ActionConflict, ActionWarnNew:
		// These actions don't require backend execution
		return nil
	default:
		return nil
	}
}

// executeCreateMR creates a new feature via MR.
func (b *Backend) executeCreateMR(ctx context.Context, action SyncAction) error {
	if action.Feature == nil {
		return errors.New("executeCreateMR: feature is nil")
	}

	// Create feature via MR workflow
	// Note: CreateFeature internally calls trackPendingMR with full MR info
	_, err := b.CreateFeature(ctx, *action.Feature)
	if err != nil {
		return fmt.Errorf("create feature MR: %w", err)
	}

	// Pending MR tracking is handled by CreateFeature -> createFeatureWithMR -> trackPendingMR
	// which has access to the full MRInfo (IID, URL, Branch)
	return nil
}

// executeUpdateMR creates an update MR.
func (b *Backend) executeUpdateMR(ctx context.Context, action SyncAction) error {
	if action.Feature == nil {
		return errors.New("executeUpdateMR: feature is nil")
	}
	if action.ServerID == "" {
		return errors.New("executeUpdateMR: server ID is empty")
	}

	// Update feature via MR workflow
	// Note: UpdateFeature internally calls trackPendingMR with full MR info
	_, err := b.UpdateFeature(ctx, action.ServerID, *action.Feature)
	if err != nil {
		return fmt.Errorf("update feature MR: %w", err)
	}

	// Pending MR tracking is handled by UpdateFeature -> updateFeatureWithMR -> trackPendingMR
	return nil
}

// executeMRMerged handles cleanup when an MR is merged.
func (b *Backend) executeMRMerged(action SyncAction) error {
	pending, err := LoadPendingMRs()
	if err != nil {
		return fmt.Errorf("load pending MRs: %w", err)
	}

	pending.Remove(action.LocalID)

	if err := pending.Save(); err != nil {
		return fmt.Errorf("save pending MRs: %w", err)
	}

	return nil
}
