package gitlab

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

// PendingMRsFile is the filename for tracking pending merge requests.
const PendingMRsFile = "pending-mrs.json"

// PendingMRsDir is the directory for featctl state files.
const PendingMRsDir = ".fas"

// PendingMRsVersion is the current schema version.
const PendingMRsVersion = "1"

// PendingMR represents a merge request that has been created but not yet merged.
type PendingMR struct {
	LocalID   string    `json:"local_id"`            // Original FT-LOCAL-* ID
	ServerID  string    `json:"server_id,omitempty"` // Assigned FT-NNNNNN ID
	MRIID     int       `json:"mr_iid"`              // GitLab MR internal ID
	MRURL     string    `json:"mr_url"`              // Full URL to MR
	Branch    string    `json:"branch"`              // Source branch name
	Operation string    `json:"operation"`           // "create", "update", "delete"
	CreatedAt time.Time `json:"created_at"`          // When MR was created
}

// PendingMRs represents the pending MRs tracking file.
type PendingMRs struct {
	Version string      `json:"version"`
	Pending []PendingMR `json:"pending"`
}

// Errors for pending MR operations.
var (
	ErrNoPendingMRs = errors.New("no pending MRs file found")
)

// pendingMRsPath returns the path to the pending MRs file.
// Creates the directory if it doesn't exist.
func pendingMRsPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	// Walk up to find git root or use cwd
	dir := cwd
	for {
		gitPath := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			// Found git root
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root, use cwd
			dir = cwd
			break
		}
		dir = parent
	}

	return filepath.Join(dir, PendingMRsDir, PendingMRsFile), nil
}

// LoadPendingMRs loads the pending MRs from disk.
// Returns empty PendingMRs if file doesn't exist.
func LoadPendingMRs() (*PendingMRs, error) {
	path, err := pendingMRsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path) //nolint:gosec // Path from trusted location
	if err != nil {
		if os.IsNotExist(err) {
			return &PendingMRs{
				Version: PendingMRsVersion,
				Pending: []PendingMR{},
			}, nil
		}
		return nil, fmt.Errorf("read pending MRs: %w", err)
	}

	var p PendingMRs
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse pending MRs: %w", err)
	}

	// Initialize if nil
	if p.Pending == nil {
		p.Pending = []PendingMR{}
	}

	return &p, nil
}

// Save writes the pending MRs to disk.
func (p *PendingMRs) Save() error {
	path, err := pendingMRsPath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if mkdirErr := os.MkdirAll(dir, 0o750); mkdirErr != nil {
		return fmt.Errorf("create directory: %w", mkdirErr)
	}

	data, marshalErr := json.MarshalIndent(p, "", "  ")
	if marshalErr != nil {
		return fmt.Errorf("marshal pending MRs: %w", marshalErr)
	}

	if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
		return fmt.Errorf("write pending MRs: %w", writeErr)
	}

	return nil
}

// Add adds a new pending MR.
func (p *PendingMRs) Add(mr PendingMR) {
	// Remove any existing entry with same local ID
	p.Remove(mr.LocalID)
	p.Pending = append(p.Pending, mr)
}

// Remove removes a pending MR by local ID.
func (p *PendingMRs) Remove(localID string) {
	filtered := make([]PendingMR, 0, len(p.Pending))
	for _, mr := range p.Pending {
		if mr.LocalID != localID {
			filtered = append(filtered, mr)
		}
	}
	p.Pending = filtered
}

// RemoveByServerID removes a pending MR by server ID.
func (p *PendingMRs) RemoveByServerID(serverID string) {
	filtered := make([]PendingMR, 0, len(p.Pending))
	for _, mr := range p.Pending {
		if mr.ServerID != serverID {
			filtered = append(filtered, mr)
		}
	}
	p.Pending = filtered
}

// FindByLocalID finds a pending MR by local ID.
func (p *PendingMRs) FindByLocalID(localID string) (*PendingMR, bool) {
	for i := range p.Pending {
		if p.Pending[i].LocalID == localID {
			return &p.Pending[i], true
		}
	}
	return nil, false
}

// FindByServerID finds a pending MR by server ID.
func (p *PendingMRs) FindByServerID(serverID string) (*PendingMR, bool) {
	for i := range p.Pending {
		if p.Pending[i].ServerID == serverID {
			return &p.Pending[i], true
		}
	}
	return nil, false
}

// FindByMRIID finds a pending MR by GitLab MR IID.
func (p *PendingMRs) FindByMRIID(iid int) (*PendingMR, bool) {
	for i := range p.Pending {
		if p.Pending[i].MRIID == iid {
			return &p.Pending[i], true
		}
	}
	return nil, false
}

// Count returns the number of pending MRs.
func (p *PendingMRs) Count() int {
	return len(p.Pending)
}

// IsEmpty returns true if there are no pending MRs.
func (p *PendingMRs) IsEmpty() bool {
	return len(p.Pending) == 0
}

// List returns all pending MRs.
func (p *PendingMRs) List() []PendingMR {
	result := make([]PendingMR, len(p.Pending))
	copy(result, p.Pending)
	return result
}

// trackPendingMR adds an MR to the pending list.
// This is called after successful MR creation to track the pending state.
func trackPendingMR(feature *backend.Feature, mrInfo *MRInfo, operation string) error {
	if feature == nil || mrInfo == nil {
		return nil
	}

	pending, err := LoadPendingMRs()
	if err != nil {
		return fmt.Errorf("load pending MRs: %w", err)
	}

	// For server-assigned IDs (FT-NNNNNN), use the ID for both local and server
	// to ensure lookups work correctly. The manifest context will have the original
	// local ID if needed.
	localID := feature.ID
	serverID := feature.ID
	if IsLocalID(feature.ID) {
		serverID = "" // Will be assigned when MR is merged
	}

	pending.Add(PendingMR{
		LocalID:   localID,
		ServerID:  serverID,
		MRIID:     mrInfo.IID,
		MRURL:     mrInfo.URL,
		Branch:    mrInfo.Branch,
		Operation: operation,
		CreatedAt: time.Now().UTC(),
	})

	if saveErr := pending.Save(); saveErr != nil {
		return fmt.Errorf("save pending MRs: %w", saveErr)
	}

	return nil
}
