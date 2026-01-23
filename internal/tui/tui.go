// Package tui provides a terminal user interface for feature-atlas using Bubble Tea.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
	"github.com/JoobyPM/feature-atlas-service/internal/cache"
	"github.com/JoobyPM/feature-atlas-service/internal/manifest"
)

const (
	debounceDelay  = 300 * time.Millisecond
	maxSuggestions = 10
	visibleItems   = 10
)

// Color constants to avoid duplication (DRY).
const (
	colorPrimary = "#7D56F4"
	colorDim     = "#666666"
	colorError   = "#FF5F87"
	colorHelp    = "#626262"
	colorWhite   = "#FFFFFF"
	colorGreen   = "#87D787"
	colorBlue    = "#87CEEB"
	colorYellow  = "#FFD787"
)

// Styles for the TUI (SST - single source of truth for styling).
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(colorPrimary)).
			MarginBottom(1)

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorPrimary)).
			Bold(true)

	itemNormalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorWhite))

	itemDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorDim))

	checkboxCheckedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorPrimary)).
				Bold(true)

	checkboxUncheckedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorDim))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorError)).
			MarginTop(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorHelp)).
			MarginTop(1)

	statusInManifestStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorGreen))

	statusOnServerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorBlue))

	statusLocalOnlyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorYellow))

	confirmBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(colorPrimary)).
			Padding(1, 2)

	confirmTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color(colorPrimary)).
				MarginBottom(1)

	confirmItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorWhite)).
				PaddingLeft(2)

	confirmHelpStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorHelp)).
				MarginTop(1)

	selectedCountStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorPrimary)).
				Bold(true)
)

// FeatureStatus indicates where a feature exists.
type FeatureStatus int

const (
	// StatusOnServer means the feature exists only on the server.
	StatusOnServer FeatureStatus = iota
	// StatusInManifest means the feature is in the local manifest (synced from server).
	StatusInManifest
	// StatusLocalOnly means the feature is local only (not synced).
	StatusLocalOnly
)

// featureItem represents a feature in the list.
type featureItem struct {
	id      string
	name    string
	summary string
	status  FeatureStatus
}

// State represents the current UI state.
type State int

// State constants for the TUI lifecycle.
const (
	StateSearching State = iota
	StateConfirm
	StateCreating // Form view for creating new features
	StateSelected
	StateQuitting
)

// ErrTUIUnexpectedModel is returned when the TUI returns an unexpected model type.
var ErrTUIUnexpectedModel = errors.New("unexpected TUI model type")

// ErrNoServerConnection is returned when operations require server but client is nil.
var ErrNoServerConnection = errors.New("no server connection")

// Result contains the outcome of the TUI session.
type Result struct {
	// Selected contains features that were selected/confirmed.
	Selected []backend.SuggestItem
	// SyncRequested is true if user chose to sync after adding.
	SyncRequested bool
	// Cancelled is true if user cancelled without selecting.
	Cancelled bool
}

// Options configures the TUI behavior.
type Options struct {
	// ManifestFeatures maps feature IDs that are already in the manifest.
	ManifestFeatures map[string]bool
	// LocalFeatures maps feature IDs that are local only (unsynced).
	LocalFeatures map[string]bool
	// SyncFlag indicates if --sync was passed (auto-sync on confirm).
	SyncFlag bool
	// Cache provides local feature caching for validation hints.
	Cache *cache.Cache
	// Manifest is used to add created features to the local manifest.
	Manifest *manifest.Manifest
	// ManifestPath is the path to save the manifest file.
	ManifestPath string
}

// selectedItem stores full data for a selected feature (SST for selections).
type selectedItem struct {
	id      string
	name    string
	summary string
}

// Model is the Bubble Tea model for the TUI.
type Model struct {
	backend          backend.FeatureBackend
	textInput        textinput.Model
	items            []featureItem           // Current list of items from API
	cursorIndex      int                     // Current cursor position in items
	selected         map[string]selectedItem // Selected features with full data (SST)
	manifestFeatures map[string]bool         // Features already in manifest
	localFeatures    map[string]bool         // Local-only features
	state            State
	syncFlag         bool // --sync flag from CLI
	syncRequested    bool // User chose 'S' in confirm
	err              error
	width            int
	height           int
	lastQuery        string
	debounceID       int
	loading          bool
	// Feature creation support
	formModel    *FormModel         // Form for creating new features (pointer: huh.Form requires stable memory)
	cache        *cache.Cache       // Local cache for validation hints
	manifest     *manifest.Manifest // Manifest for adding created features
	manifestPath string             // Path to save manifest
	cacheUpdated bool               // Tracks if we've already added created feature to cache
}

// debounceMsg is sent after the debounce delay.
type debounceMsg struct {
	query string
	id    int
}

// suggestionsMsg contains the suggestions from the API.
type suggestionsMsg struct {
	items []backend.SuggestItem
	err   error
}

// New creates a new TUI model.
func New(b backend.FeatureBackend, opts Options) Model {
	ti := textinput.New()
	ti.Placeholder = "Search features..."
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorPrimary))
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorWhite))

	// Initialize maps with nil-safe defaults
	manifestFeatures := opts.ManifestFeatures
	if manifestFeatures == nil {
		manifestFeatures = make(map[string]bool)
	}
	localFeatures := opts.LocalFeatures
	if localFeatures == nil {
		localFeatures = make(map[string]bool)
	}

	return Model{
		backend:          b,
		textInput:        ti,
		items:            nil,
		cursorIndex:      0,
		selected:         make(map[string]selectedItem),
		manifestFeatures: manifestFeatures,
		localFeatures:    localFeatures,
		state:            StateSearching,
		syncFlag:         opts.SyncFlag,
		width:            80,
		height:           24,
		cache:            opts.Cache,
		manifest:         opts.Manifest,
		manifestPath:     opts.ManifestPath,
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textinput.Blink,
		m.fetchSuggestions(""),
	}

	// Check if cache needs refresh
	if m.cache != nil && m.cache.IsStale() {
		cmds = append(cmds, m.refreshCacheCmd())
	}

	return tea.Batch(cmds...)
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle StateCreating FIRST - forward ALL messages to form
	if m.state == StateCreating && m.formModel != nil {
		// Check for global quit (Ctrl+C)
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == keyCtrlC {
			m.state = StateQuitting
			return m, tea.Quit
		}

		// Delegate ALL messages to form (KeyMsg, featureCreatedMsg, duplicateCheckResultMsg, etc.)
		cmd := m.formModel.Update(msg)

		// Check if user acknowledged success and wants to return
		if m.formModel.Done() {
			m.state = StateSearching
			m.formModel = nil
			m.cacheUpdated = false // Reset for next form
			return m, nil
		}

		// Check if form completed successfully (first time only)
		// cacheUpdated flag prevents duplicate adds when cacheSavedMsg arrives
		if m.formModel.Completed() && !m.cacheUpdated {
			if created := m.formModel.GetCreatedFeature(); created != nil {
				var cmds []tea.Cmd
				if cmd != nil {
					cmds = append(cmds, cmd)
				}

				// 1. Add to manifest (source of truth - async to avoid blocking)
				if m.manifest != nil {
					cmds = append(cmds, m.saveManifestCmd(manifest.Feature{
						ID:       created.ID,
						Name:     created.Name,
						Summary:  created.Summary,
						Owner:    created.Owner,
						Tags:     created.Tags,
						IsSynced: true, // Created on server, so it's synced
					}))
				}

				// 2. Add to cache (performance hint)
				if m.cache != nil {
					m.cache.Add(cache.CachedFeature{
						ID:      created.ID,
						Name:    created.Name,
						Summary: created.Summary,
					})
					cmds = append(cmds, m.saveCacheCmd())
				}

				m.cacheUpdated = true // Mark as done
				return m, tea.Batch(cmds...)
			}
		}

		// Check if form was cancelled (huh handles Esc → StateAborted)
		if m.formModel.Cancelled() {
			m.state = StateSearching
			m.formModel = nil
			m.cacheUpdated = false // Reset for next form
			return m, nil
		}

		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case debounceMsg:
		if msg.id == m.debounceID {
			return m, m.fetchSuggestions(msg.query)
		}
		return m, nil

	case suggestionsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// Convert API items to internal items
		m.items = make([]featureItem, len(msg.items))
		for i, item := range msg.items {
			m.items[i] = featureItem{
				id:      item.ID,
				name:    item.Name,
				summary: item.Summary,
				status:  m.getFeatureStatus(item.ID),
			}
		}
		// Reset cursor if it's out of bounds
		if m.cursorIndex >= len(m.items) {
			m.cursorIndex = 0
		}
		m.err = nil
		return m, nil

	case cacheRefreshResultMsg:
		if m.cache != nil && msg.err == nil {
			// Update cache data (in Update, not in Cmd - safe!)
			m.cache.Update(msg.features, msg.serverURL, msg.complete)
			// Save to disk via command
			return m, m.saveCacheCmd()
		}
		return m, nil

	case cacheSavedMsg:
		// Cache saved (or failed - non-critical, don't block)
		return m, nil

	case manifestSavedMsg:
		// Manifest saved (or failed - feature already on server, this is secondary)
		return m, nil
	}

	return m, nil
}

// handleKeyMsg processes keyboard input based on current state.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle confirmation mode keys
	if m.state == StateConfirm {
		return m.handleConfirmKeys(msg)
	}

	// Global quit keys
	switch msg.String() {
	case keyCtrlC:
		m.state = StateQuitting
		return m, tea.Quit
	case keyEsc:
		// If search has content, clear it; otherwise quit
		if m.textInput.Value() != "" {
			m.textInput.SetValue("")
			m.lastQuery = ""
			m.debounceID++
			m.loading = true
			return m, m.debounceSearch("", m.debounceID)
		}
		m.state = StateQuitting
		return m, tea.Quit
	}

	// Arrow keys ALWAYS navigate (don't conflict with typing)
	//nolint:exhaustive // Only handling up/down arrows here, rest handled below
	switch msg.Type {
	case tea.KeyUp:
		return m.navigateUp()
	case tea.KeyDown:
		return m.navigateDown()
	default:
		// Other key types handled below
	}

	// Special keys for selection (non-typing keys)
	switch msg.String() {
	case " ": // Space - toggle selection
		return m.handleSpaceToggle()

	case "enter":
		return m.handleEnter()

	case "ctrl+a": // Select all visible
		return m.handleSelectAll()

	case "ctrl+n": // Deselect all
		return m.handleDeselectAll()

	case "n": // Open feature creation form
		m.state = StateCreating
		m.formModel = NewFormModel(m.backend, m.cache)
		return m, m.formModel.Init()
	}

	// All other keys go to text input for searching
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)

	// Check if query changed - trigger search
	newQuery := m.textInput.Value()
	if newQuery != m.lastQuery {
		m.lastQuery = newQuery
		m.debounceID++
		m.loading = true
		return m, tea.Batch(cmd, m.debounceSearch(newQuery, m.debounceID))
	}

	return m, cmd
}

// navigateUp moves cursor up with bounds checking.
func (m Model) navigateUp() (tea.Model, tea.Cmd) {
	if len(m.items) > 0 && m.cursorIndex > 0 {
		m.cursorIndex--
	}
	return m, nil
}

// navigateDown moves cursor down with bounds checking.
func (m Model) navigateDown() (tea.Model, tea.Cmd) {
	if len(m.items) > 0 && m.cursorIndex < len(m.items)-1 {
		m.cursorIndex++
	}
	return m, nil
}

// handleConfirmKeys handles key presses in confirmation mode.
func (m Model) handleConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.state = StateSelected
		return m, tea.Quit

	case "s", "S":
		m.syncRequested = true
		m.state = StateSelected
		return m, tea.Quit

	case "n", "N", keyEsc, "q":
		m.state = StateSearching
		return m, nil

	case keyCtrlC:
		m.state = StateQuitting
		return m, tea.Quit
	}
	return m, nil
}

// handleEnter processes the Enter key.
func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.state != StateSearching {
		return m, nil
	}

	// If nothing is selected and we have items, select the current item
	if len(m.selected) == 0 && len(m.items) > 0 && m.cursorIndex < len(m.items) {
		item := m.items[m.cursorIndex]
		m.selected[item.id] = selectedItem{
			id:      item.id,
			name:    item.name,
			summary: item.summary,
		}
	}

	// Must have selections to proceed
	if len(m.selected) == 0 {
		return m, nil
	}

	// If --sync flag was passed, skip confirmation
	if m.syncFlag {
		m.syncRequested = true
		m.state = StateSelected
		return m, tea.Quit
	}

	// Show confirmation dialog
	m.state = StateConfirm
	return m, nil
}

// handleSpaceToggle toggles selection on the current item.
func (m Model) handleSpaceToggle() (tea.Model, tea.Cmd) {
	if m.state != StateSearching || len(m.items) == 0 {
		return m, nil
	}

	if m.cursorIndex >= len(m.items) {
		return m, nil
	}

	item := m.items[m.cursorIndex]

	// Toggle selection - store full item data (SST)
	if _, exists := m.selected[item.id]; exists {
		delete(m.selected, item.id)
	} else {
		m.selected[item.id] = selectedItem{
			id:      item.id,
			name:    item.name,
			summary: item.summary,
		}
	}

	return m, nil
}

// handleSelectAll selects all visible items.
func (m Model) handleSelectAll() (tea.Model, tea.Cmd) {
	if m.state != StateSearching {
		return m, nil
	}

	for _, item := range m.items {
		m.selected[item.id] = selectedItem{
			id:      item.id,
			name:    item.name,
			summary: item.summary,
		}
	}
	return m, nil
}

// handleDeselectAll deselects all items.
func (m Model) handleDeselectAll() (tea.Model, tea.Cmd) {
	if m.state != StateSearching {
		return m, nil
	}

	clear(m.selected)
	return m, nil
}

// getFeatureStatus determines the status of a feature.
func (m Model) getFeatureStatus(id string) FeatureStatus {
	if m.localFeatures[id] {
		return StatusLocalOnly
	}
	if m.manifestFeatures[id] {
		return StatusInManifest
	}
	return StatusOnServer
}

// View renders the UI.
func (m Model) View() string {
	if m.state == StateQuitting {
		return ""
	}

	if m.state == StateConfirm {
		return m.viewConfirmation()
	}

	if m.state == StateCreating && m.formModel != nil {
		return m.formModel.View()
	}

	if m.state == StateSelected {
		return m.viewSelected()
	}

	return m.viewSearch()
}

// viewSearch renders the search interface.
func (m Model) viewSearch() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("🔍 Feature Atlas"))
	b.WriteString("\n\n")
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(itemDimStyle.Render("Searching..."))
		b.WriteString("\n")
	} else if m.err != nil {
		b.WriteString(errorStyle.Render("Error: " + m.err.Error()))
		b.WriteString("\n")
	} else if len(m.items) == 0 {
		b.WriteString(itemDimStyle.Render("No results found"))
		b.WriteString("\n")
	} else {
		// Render items manually - direct access to m.selected (SST)
		b.WriteString(m.renderItemList())
	}

	// Selected count - uses m.selected directly
	selectedCount := len(m.selected)
	if selectedCount > 0 {
		b.WriteString("\n")
		b.WriteString(selectedCountStyle.Render(fmt.Sprintf("Selected: %d", selectedCount)))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓: navigate • Space: toggle • n: new • Ctrl+A: all • Ctrl+N: none • Enter: confirm • Esc: quit"))

	return b.String()
}

// renderItemList renders the list items with checkboxes and status indicators.
// This method has direct access to m.selected, ensuring consistency.
func (m Model) renderItemList() string {
	var b strings.Builder

	// Calculate visible window with proper scrolling
	start, end := m.calculateVisibleRange()

	// Show scroll indicator if not at top
	if start > 0 {
		b.WriteString(itemDimStyle.Render(fmt.Sprintf("  ↑ %d more above", start)))
		b.WriteString("\n")
	}

	for i := start; i < end; i++ {
		item := m.items[i]

		// Checkbox - reads directly from m.selected (SST)
		var checkbox string
		if _, exists := m.selected[item.id]; exists {
			checkbox = checkboxCheckedStyle.Render("[✓]")
		} else {
			checkbox = checkboxUncheckedStyle.Render("[ ]")
		}

		// Status indicator
		status := m.renderStatus(item.status)

		// Build line content
		content := fmt.Sprintf("%s %s - %s  %s", checkbox, item.id, item.name, status)

		// Apply cursor styling
		if i == m.cursorIndex {
			b.WriteString(cursorStyle.Render("> " + content))
		} else {
			b.WriteString(itemNormalStyle.Render("  " + content))
		}
		b.WriteString("\n")
	}

	// Show scroll indicator if not at bottom
	remaining := len(m.items) - end
	if remaining > 0 {
		b.WriteString(itemDimStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		b.WriteString("\n")
	}

	return b.String()
}

// calculateVisibleRange returns the start and end indices for visible items.
// Ensures cursor is always visible with proper scrolling.
func (m Model) calculateVisibleRange() (start, end int) {
	total := len(m.items)
	if total == 0 {
		return 0, 0
	}
	if total <= visibleItems {
		return 0, total
	}

	// Keep cursor visible by centering it when possible
	half := visibleItems / 2

	// Start position based on cursor
	start = m.cursorIndex - half
	if start < 0 {
		start = 0
	}

	end = start + visibleItems
	if end > total {
		end = total
		start = end - visibleItems
		if start < 0 {
			start = 0
		}
	}

	return start, end
}

// renderStatus renders the status indicator for a feature.
func (m Model) renderStatus(status FeatureStatus) string {
	switch status {
	case StatusInManifest:
		return statusInManifestStyle.Render("[in manifest]")
	case StatusLocalOnly:
		return statusLocalOnlyStyle.Render("[local only]")
	case StatusOnServer:
		return statusOnServerStyle.Render("[on server]")
	default:
		return ""
	}
}

// viewConfirmation renders the confirmation dialog.
func (m Model) viewConfirmation() string {
	var b strings.Builder

	// Get all selected items (from SST map)
	items := m.getSelectedItems()

	title := fmt.Sprintf("Add %d feature(s) to manifest?", len(items))
	b.WriteString(confirmTitleStyle.Render(title))
	b.WriteString("\n\n")

	for _, item := range items {
		line := fmt.Sprintf("• %s - %s", item.ID, item.Name)
		b.WriteString(confirmItemStyle.Render(line))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(confirmHelpStyle.Render("[Y]es  [N]o  [S]ync after adding"))

	return confirmBoxStyle.Render(b.String())
}

// viewSelected renders the selection result.
func (m Model) viewSelected() string {
	var b strings.Builder

	items := m.getSelectedItems()

	b.WriteString(titleStyle.Render("✓ Features Selected"))
	b.WriteString("\n\n")

	for _, item := range items {
		line := fmt.Sprintf("  • %s - %s", item.ID, item.Name)
		b.WriteString(cursorStyle.Render(line))
		b.WriteString("\n")
	}

	if m.syncRequested {
		b.WriteString("\n")
		b.WriteString(itemDimStyle.Render("Sync requested - will sync to server"))
	}

	return b.String()
}

// getSelectedItems returns all selected items as backend items (from SST map).
func (m Model) getSelectedItems() []backend.SuggestItem {
	items := make([]backend.SuggestItem, 0, len(m.selected))
	for _, sel := range m.selected {
		items = append(items, backend.SuggestItem{
			ID:      sel.id,
			Name:    sel.name,
			Summary: sel.summary,
		})
	}
	return items
}

// debounceSearch returns a command that triggers a search after a delay.
func (m Model) debounceSearch(query string, id int) tea.Cmd {
	return tea.Tick(debounceDelay, func(_ time.Time) tea.Msg {
		return debounceMsg{query: query, id: id}
	})
}

// fetchSuggestions fetches suggestions from the backend.
func (m Model) fetchSuggestions(query string) tea.Cmd {
	// Capture backend for closure (defensive: handle nil)
	backendRef := m.backend
	return func() tea.Msg {
		if backendRef == nil {
			return suggestionsMsg{err: ErrNoServerConnection}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		items, err := backendRef.Suggest(ctx, query, maxSuggestions)
		return suggestionsMsg{items: items, err: err}
	}
}

// GetResult returns the TUI result.
func (m Model) GetResult() Result {
	if m.state == StateQuitting {
		return Result{Cancelled: true}
	}

	return Result{
		Selected:      m.getSelectedItems(),
		SyncRequested: m.syncRequested || m.syncFlag,
		Cancelled:     m.state != StateSelected,
	}
}

// Run starts the TUI and returns the result.
func Run(b backend.FeatureBackend, opts Options) (Result, error) {
	model := New(b, opts)
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return Result{Cancelled: true}, fmt.Errorf("TUI error: %w", err)
	}

	m, ok := finalModel.(Model)
	if !ok {
		return Result{Cancelled: true}, ErrTUIUnexpectedModel
	}

	return m.GetResult(), nil
}

// RunLegacy provides backward-compatible single-select behavior.
// Deprecated: Use Run with Options instead.
func RunLegacy(b backend.FeatureBackend) (*backend.SuggestItem, error) {
	result, err := Run(b, Options{})
	if err != nil {
		return nil, err
	}

	if result.Cancelled || len(result.Selected) == 0 {
		return nil, nil //nolint:nilnil // nil pointer is valid for "no selection" case
	}

	return &result.Selected[0], nil
}

// Messages for cache operations.
type cacheRefreshResultMsg struct {
	features  []cache.CachedFeature
	serverURL string
	complete  bool
	err       error
}

type cacheSavedMsg struct{ err error }

type manifestSavedMsg struct{ err error }

// refreshCacheCmd fetches features and returns result via message.
func (m Model) refreshCacheCmd() tea.Cmd {
	// Capture dependencies for closure
	backendRef := m.backend

	return func() tea.Msg {
		if backendRef == nil {
			return cacheRefreshResultMsg{err: ErrNoServerConnection}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Fetch features (API doesn't support pagination, so single request)
		features, err := backendRef.Search(ctx, "", cacheRefreshLimit)
		if err != nil {
			return cacheRefreshResultMsg{err: err}
		}

		allFeatures := make([]cache.CachedFeature, len(features))
		for i, f := range features {
			allFeatures[i] = cache.CachedFeature{
				ID:      f.ID,
				Name:    f.Name,
				Summary: f.Summary,
			}
		}

		// Mark incomplete if we hit limit (server may have more features)
		// Use InstanceID for cache isolation across different backend instances
		return cacheRefreshResultMsg{
			features:  allFeatures,
			serverURL: backendRef.InstanceID(),
			complete:  len(features) < cacheRefreshLimit,
		}
	}
}

// saveCacheCmd saves the cache to disk asynchronously.
func (m Model) saveCacheCmd() tea.Cmd {
	cacheRef := m.cache // Capture for closure
	return func() tea.Msg {
		if cacheRef == nil {
			return cacheSavedMsg{}
		}
		err := cacheRef.Save()
		return cacheSavedMsg{err: err}
	}
}

// saveManifestCmd adds a feature to the manifest and saves it asynchronously.
func (m Model) saveManifestCmd(feature manifest.Feature) tea.Cmd {
	manifestRef := m.manifest // Capture for closure
	manifestPath := m.manifestPath
	return func() tea.Msg {
		if manifestRef == nil {
			return manifestSavedMsg{}
		}
		if err := manifestRef.AddSyncedFeature(feature); err != nil {
			return manifestSavedMsg{err: fmt.Errorf("add feature: %w", err)}
		}
		if manifestPath != "" {
			// Use SaveWithLock for inter-process safety
			if err := manifestRef.SaveWithLock(manifestPath); err != nil {
				return manifestSavedMsg{err: fmt.Errorf("save manifest: %w", err)}
			}
		}
		return manifestSavedMsg{}
	}
}
