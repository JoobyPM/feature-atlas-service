package testutil

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitLabMockServer provides a mock GitLab API for testing.
type GitLabMockServer struct {
	Server *httptest.Server

	mu       sync.RWMutex
	features map[string]*MockFeature
	branches map[string]bool
	mrs      map[int]*MockMR
	nextMRID int
	nextID   int

	// Hooks for custom behavior
	OnListFiles  func() error
	OnCreateFile func(path string) error
	OnCreateMR   func(title string) error
	OnGetMR      func(iid int) error
}

// MockFeature represents a feature in the mock GitLab repo.
type MockFeature struct {
	ID        string   `yaml:"id" json:"id"`
	Name      string   `yaml:"name" json:"name"`
	Summary   string   `yaml:"summary" json:"summary"`
	Owner     string   `yaml:"owner,omitempty" json:"owner,omitempty"`
	Tags      []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	CreatedAt string   `yaml:"created_at" json:"created_at"`
	UpdatedAt string   `yaml:"updated_at" json:"updated_at"`
}

// MockMR represents a merge request in the mock.
type MockMR struct {
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	State        string `json:"state"`
	WebURL       string `json:"web_url"`
}

// NewGitLabMockServer creates a new mock GitLab server with seed data.
func NewGitLabMockServer(seedCount int) *GitLabMockServer {
	mock := &GitLabMockServer{
		features: make(map[string]*MockFeature),
		branches: make(map[string]bool),
		mrs:      make(map[int]*MockMR),
		nextMRID: 1,
		nextID:   seedCount + 1,
	}

	// Seed features
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 1; i <= seedCount; i++ {
		id := fmt.Sprintf("FT-%06d", i)
		mock.features[id] = &MockFeature{
			ID:        id,
			Name:      fmt.Sprintf("Feature %d", i),
			Summary:   fmt.Sprintf("Description for feature %d", i),
			Owner:     "Test Team",
			Tags:      []string{"seed", "test"},
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	// Create main branch
	mock.branches["main"] = true

	mock.Server = httptest.NewServer(http.HandlerFunc(mock.handleRequest))
	return mock
}

// Close shuts down the mock server.
func (m *GitLabMockServer) Close() {
	m.Server.Close()
}

// URL returns the mock server URL.
func (m *GitLabMockServer) URL() string {
	return m.Server.URL
}

// AddFeature adds a feature to the mock.
func (m *GitLabMockServer) AddFeature(f *MockFeature) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.features[f.ID] = f
}

// GetFeature returns a feature from the mock.
func (m *GitLabMockServer) GetFeature(id string) *MockFeature {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.features[id]
}

// SetMRState sets the state of a merge request.
func (m *GitLabMockServer) SetMRState(iid int, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mr, ok := m.mrs[iid]; ok {
		mr.State = state
	}
}

// handleRequest routes requests to the appropriate handler.
func (m *GitLabMockServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Route based on path patterns
	switch {
	case strings.Contains(path, "/repository/tree"):
		m.handleListTree(w, r)
	case strings.Contains(path, "/repository/files"):
		m.handleRepositoryFiles(w, r)
	case strings.Contains(path, "/repository/branches"):
		m.handleBranches(w, r)
	case strings.Contains(path, "/merge_requests"):
		m.handleMergeRequests(w, r)
	case strings.Contains(path, "/members"):
		m.handleMembers(w, r)
	case strings.Contains(path, "/user"):
		m.handleCurrentUser(w, r)
	case strings.Contains(path, "/users"):
		m.handleUsers(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleListTree returns the list of feature files.
func (m *GitLabMockServer) handleListTree(w http.ResponseWriter, _ *http.Request) {
	if m.OnListFiles != nil {
		if err := m.OnListFiles(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var files []map[string]any
	for id := range m.features {
		files = append(files, map[string]any{
			"id":   id,
			"name": id + ".yaml",
			"type": "blob",
			"path": "features/" + id + ".yaml",
			"mode": "100644",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(files)
}

// handleRepositoryFiles handles file read/create/update/delete.
func (m *GitLabMockServer) handleRepositoryFiles(w http.ResponseWriter, r *http.Request) {
	// Extract file path from URL
	path := r.URL.Path
	parts := strings.Split(path, "/repository/files/")
	if len(parts) < 2 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	filePath := strings.TrimSuffix(parts[1], "/raw")
	// URL decode the path (features%2FFT-000001.yaml -> features/FT-000001.yaml)
	filePath = strings.ReplaceAll(filePath, "%2F", "/")

	switch r.Method {
	case http.MethodGet:
		m.handleGetFile(w, r, filePath)
	case http.MethodPost:
		m.handleCreateFile(w, r, filePath)
	case http.MethodPut:
		m.handleUpdateFile(w, r, filePath)
	case http.MethodDelete:
		m.handleDeleteFile(w, r, filePath)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetFile returns file content.
func (m *GitLabMockServer) handleGetFile(w http.ResponseWriter, r *http.Request, filePath string) {
	// Check if it's a raw request
	if strings.HasSuffix(r.URL.Path, "/raw") {
		m.handleGetFileRaw(w, filePath)
		return
	}

	// Return file metadata
	id := extractFeatureID(filePath)
	m.mu.RLock()
	feature, ok := m.features[id]
	m.mu.RUnlock()

	if !ok {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	content := formatFeatureYAML(feature)
	// GitLab API returns base64 encoded content
	encodedContent := base64.StdEncoding.EncodeToString([]byte(content))
	resp := map[string]any{
		"file_name":      id + ".yaml",
		"file_path":      filePath,
		"size":           len(content),
		"encoding":       "base64",
		"content_sha256": "abc123",
		"ref":            "main",
		"blob_id":        "def456",
		"commit_id":      "ghi789",
		"last_commit_id": "jkl012",
		"content":        encodedContent,
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(resp)
}

// handleGetFileRaw returns raw file content.
func (m *GitLabMockServer) handleGetFileRaw(w http.ResponseWriter, filePath string) {
	id := extractFeatureID(filePath)
	m.mu.RLock()
	feature, ok := m.features[id]
	m.mu.RUnlock()

	if !ok {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(formatFeatureYAML(feature)))
}

// handleCreateFile creates a new file.
func (m *GitLabMockServer) handleCreateFile(w http.ResponseWriter, r *http.Request, filePath string) {
	if m.OnCreateFile != nil {
		if err := m.OnCreateFile(filePath); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
	}

	var req struct {
		Branch        string `json:"branch"`
		Content       string `json:"content"`
		CommitMessage string `json:"commit_message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Verify branch exists
	m.mu.RLock()
	_, branchExists := m.branches[req.Branch]
	m.mu.RUnlock()

	if !branchExists {
		http.Error(w, "branch not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(map[string]any{
		"file_path": filePath,
		"branch":    req.Branch,
	})
}

// handleUpdateFile updates a file.
func (m *GitLabMockServer) handleUpdateFile(w http.ResponseWriter, r *http.Request, filePath string) {
	var req struct {
		Branch        string `json:"branch"`
		Content       string `json:"content"`
		CommitMessage string `json:"commit_message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(map[string]any{
		"file_path": filePath,
		"branch":    req.Branch,
	})
}

// handleDeleteFile deletes a file.
func (m *GitLabMockServer) handleDeleteFile(w http.ResponseWriter, r *http.Request, _ string) {
	var req struct {
		Branch        string `json:"branch"`
		CommitMessage string `json:"commit_message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleBranches handles branch operations.
func (m *GitLabMockServer) handleBranches(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		m.handleCreateBranch(w, r)
	case http.MethodDelete:
		m.handleDeleteBranch(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreateBranch creates a new branch.
func (m *GitLabMockServer) handleCreateBranch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Branch string `json:"branch"`
		Ref    string `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.branches[req.Branch] = true
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":   req.Branch,
		"commit": map[string]any{"id": "abc123"},
	})
}

// handleDeleteBranch deletes a branch.
func (m *GitLabMockServer) handleDeleteBranch(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// handleMergeRequests handles MR operations.
func (m *GitLabMockServer) handleMergeRequests(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Check if it's a specific MR (has IID in path)
	if strings.Contains(path, "/merge_requests/") {
		parts := strings.Split(path, "/merge_requests/")
		if len(parts) >= 2 {
			iidStr := strings.Split(parts[1], "/")[0]
			iid, err := strconv.Atoi(iidStr)
			if err == nil {
				m.handleSpecificMR(w, r, iid)
				return
			}
		}
	}

	switch r.Method {
	case http.MethodPost:
		m.handleCreateMR(w, r)
	case http.MethodGet:
		m.handleListMRs(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreateMR creates a new MR.
func (m *GitLabMockServer) handleCreateMR(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceBranch       string   `json:"source_branch"`
		TargetBranch       string   `json:"target_branch"`
		Title              string   `json:"title"`
		Description        string   `json:"description"`
		RemoveSourceBranch bool     `json:"remove_source_branch"`
		Labels             []string `json:"labels"`
		AssigneeID         int      `json:"assignee_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if m.OnCreateMR != nil {
		if err := m.OnCreateMR(req.Title); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
	}

	m.mu.Lock()
	iid := m.nextMRID
	m.nextMRID++
	mr := &MockMR{
		IID:          iid,
		Title:        req.Title,
		Description:  req.Description,
		SourceBranch: req.SourceBranch,
		TargetBranch: req.TargetBranch,
		State:        "opened",
		WebURL:       fmt.Sprintf("%s/-/merge_requests/%d", m.Server.URL, iid),
	}
	m.mrs[iid] = mr
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(mr)
}

// handleListMRs lists MRs.
func (m *GitLabMockServer) handleListMRs(w http.ResponseWriter, _ *http.Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mrs := make([]*MockMR, 0, len(m.mrs))
	for _, mr := range m.mrs {
		mrs = append(mrs, mr)
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(mrs)
}

// handleSpecificMR handles operations on a specific MR.
func (m *GitLabMockServer) handleSpecificMR(w http.ResponseWriter, r *http.Request, iid int) {
	if m.OnGetMR != nil {
		if err := m.OnGetMR(iid); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}

	m.mu.RLock()
	mr, ok := m.mrs[iid]
	m.mu.RUnlock()

	if !ok {
		http.Error(w, "MR not found", http.StatusNotFound)
		return
	}

	// Check if this is a diffs request
	if strings.Contains(r.URL.Path, "/diffs") {
		m.handleMRDiffs(w, mr)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(mr)
}

// handleMRDiffs returns MR diffs.
func (m *GitLabMockServer) handleMRDiffs(w http.ResponseWriter, _ *MockMR) {
	// Return a mock diff showing a feature file
	diffs := []map[string]any{
		{
			"old_path": "",
			"new_path": "features/FT-000001.yaml",
			"diff":     "@@ -0,0 +1,5 @@\n+id: FT-000001\n+name: Test\n+summary: Test feature\n",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(diffs)
}

// handleMembers handles project member requests.
func (m *GitLabMockServer) handleMembers(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]any{
		"id":           1,
		"username":     "testuser",
		"access_level": 40, // Maintainer
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(resp)
}

// handleCurrentUser returns current user info.
func (m *GitLabMockServer) handleCurrentUser(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]any{
		"id":       1,
		"username": "testuser",
		"name":     "Test User",
		"email":    "test@example.com",
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(resp)
}

// handleUsers handles user search.
func (m *GitLabMockServer) handleUsers(w http.ResponseWriter, _ *http.Request) {
	resp := []map[string]any{
		{
			"id":       1,
			"username": "testuser",
			"name":     "Test User",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // Test mock, error handling not critical
	_ = json.NewEncoder(w).Encode(resp)
}

// extractFeatureID extracts the feature ID from a file path.
func extractFeatureID(filePath string) string {
	// features/FT-000001.yaml -> FT-000001
	base := strings.TrimPrefix(filePath, "features/")
	return strings.TrimSuffix(base, ".yaml")
}

// formatFeatureYAML formats a feature as YAML.
func formatFeatureYAML(f *MockFeature) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("id: %s\n", f.ID))
	sb.WriteString(fmt.Sprintf("name: %s\n", f.Name))
	sb.WriteString(fmt.Sprintf("summary: %s\n", f.Summary))
	if f.Owner != "" {
		sb.WriteString(fmt.Sprintf("owner: %s\n", f.Owner))
	}
	if len(f.Tags) > 0 {
		sb.WriteString("tags:\n")
		for _, tag := range f.Tags {
			sb.WriteString(fmt.Sprintf("  - %s\n", tag))
		}
	}
	sb.WriteString(fmt.Sprintf("created_at: %s\n", f.CreatedAt))
	sb.WriteString(fmt.Sprintf("updated_at: %s\n", f.UpdatedAt))
	return sb.String()
}
