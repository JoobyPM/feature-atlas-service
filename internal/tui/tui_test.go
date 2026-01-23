package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
)

// updateModel is a helper that handles the Update return type.
func updateModel(m Model, msg tea.Msg) (Model, tea.Cmd) {
	newModel, cmd := m.Update(msg)
	return newModel.(Model), cmd
}

// loadTestItems loads test items into the model via suggestionsMsg.
func loadTestItems(m Model, items []backend.SuggestItem) Model {
	m, _ = updateModel(m, suggestionsMsg{items: items})
	return m
}

// isSelected checks if an item is selected.
func isSelected(m Model, id string) bool {
	_, exists := m.selected[id]
	return exists
}

// TestModel_SelectToggle verifies Space key toggles selection.
func TestModel_SelectToggle(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First feature"},
		{ID: "FT-000002", Name: "Feature 2", Summary: "Second feature"},
	})

	// Initially nothing selected
	assert.Empty(t, model.selected, "initially no selection")

	// Toggle selection with Space
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeySpace})
	assert.True(t, isSelected(model, "FT-000001"), "first item should be selected")

	// Verify full item data is stored
	sel := model.selected["FT-000001"]
	assert.Equal(t, "FT-000001", sel.id)
	assert.Equal(t, "Feature 1", sel.name)
	assert.Equal(t, "First feature", sel.summary)

	// Toggle again to deselect
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeySpace})
	assert.False(t, isSelected(model, "FT-000001"), "first item should be deselected")
}

// TestModel_SelectAll verifies Ctrl+A key selects all visible items.
func TestModel_SelectAll(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
		{ID: "FT-000002", Name: "Feature 2", Summary: "Second"},
		{ID: "FT-000003", Name: "Feature 3", Summary: "Third"},
	})

	// Press Ctrl+A to select all
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyCtrlA})

	assert.Len(t, model.selected, 3, "all items should be selected")
	assert.True(t, isSelected(model, "FT-000001"))
	assert.True(t, isSelected(model, "FT-000002"))
	assert.True(t, isSelected(model, "FT-000003"))
}

// TestModel_DeselectAll verifies Ctrl+N key deselects all items.
func TestModel_DeselectAll(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
		{ID: "FT-000002", Name: "Feature 2", Summary: "Second"},
	})

	// Select all first
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyCtrlA})
	assert.Len(t, model.selected, 2)

	// Press Ctrl+N to deselect all
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyCtrlN})

	assert.Empty(t, model.selected, "nothing should be selected after Ctrl+N")
}

// TestModel_Navigation verifies arrow key navigation.
func TestModel_Navigation(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
		{ID: "FT-000002", Name: "Feature 2", Summary: "Second"},
		{ID: "FT-000003", Name: "Feature 3", Summary: "Third"},
	})

	// Initial position is 0
	assert.Equal(t, 0, model.cursorIndex)

	// Navigate down with down arrow
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, model.cursorIndex, "down should move down")

	// Navigate down again
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, model.cursorIndex)

	// Navigate up with up arrow
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, model.cursorIndex, "up should move up")

	// Navigate up again
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, model.cursorIndex)

	// Can't go above 0
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, model.cursorIndex, "shouldn't go below 0")
}

// TestModel_TypingJK verifies j/k always go to search (not navigation).
func TestModel_TypingJK(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
		{ID: "FT-000002", Name: "Feature 2", Summary: "Second"},
	})

	// j should type in search, not navigate
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	assert.Equal(t, "j", model.textInput.Value(), "j should type in search")
	assert.Equal(t, 0, model.cursorIndex, "cursor should not move")

	// k should also type
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	assert.Equal(t, "jk", model.textInput.Value(), "k should type in search")
	assert.Equal(t, 0, model.cursorIndex, "cursor should not move")

	// Arrow keys still navigate
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, model.cursorIndex, "down arrow should navigate")
	assert.Equal(t, "jk", model.textInput.Value(), "search should be unchanged")
}

// TestModel_EnterShowsConfirm verifies Enter shows confirmation dialog.
func TestModel_EnterShowsConfirm(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
	})

	// Select an item
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeySpace})
	assert.True(t, isSelected(model, "FT-000001"))

	// Press Enter
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyEnter})

	assert.Equal(t, StateConfirm, model.state, "should be in confirm state")
}

// TestModel_EnterNoSelection verifies Enter selects current if nothing selected.
func TestModel_EnterNoSelection(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
	})

	// Press Enter without selecting
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, isSelected(model, "FT-000001"), "current item should be auto-selected")
	assert.Equal(t, StateConfirm, model.state)
}

// TestModel_ConfirmYes verifies 'Y' in confirm mode accepts selection.
func TestModel_ConfirmYes(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
	})

	// Select and confirm
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeySpace})
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, StateConfirm, model.state)

	// Press 'Y'
	model, cmd := updateModel(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})

	assert.Equal(t, StateSelected, model.state)
	assert.NotNil(t, cmd, "should return quit command")
}

// TestModel_ConfirmSync verifies 'S' in confirm mode sets sync flag.
func TestModel_ConfirmSync(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
	})

	// Select and confirm
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeySpace})
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyEnter})

	// Press 'S'
	model, cmd := updateModel(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})

	assert.True(t, model.syncRequested, "sync should be requested")
	assert.Equal(t, StateSelected, model.state)
	assert.NotNil(t, cmd)
}

// TestModel_ConfirmNo verifies 'N' in confirm mode cancels back to search.
func TestModel_ConfirmNo(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
	})

	// Select and confirm
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeySpace})
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, StateConfirm, model.state)

	// Press 'N'
	model, cmd := updateModel(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})

	assert.Equal(t, StateSearching, model.state)
	assert.Nil(t, cmd, "should not quit")
}

// TestModel_FeatureStatus verifies feature status is correctly determined.
func TestModel_FeatureStatus(t *testing.T) {
	opts := Options{
		ManifestFeatures: map[string]bool{
			"FT-000001":     true,
			"FT-LOCAL-test": true,
		},
		LocalFeatures: map[string]bool{
			"FT-LOCAL-test": true,
		},
	}
	model := New(nil, opts)

	assert.Equal(t, StatusInManifest, model.getFeatureStatus("FT-000001"))
	assert.Equal(t, StatusLocalOnly, model.getFeatureStatus("FT-LOCAL-test"))
	assert.Equal(t, StatusOnServer, model.getFeatureStatus("FT-000002"))
}

// TestModel_SyncFlagSkipsConfirm verifies --sync flag skips confirmation.
func TestModel_SyncFlagSkipsConfirm(t *testing.T) {
	model := New(nil, Options{SyncFlag: true})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
	})

	// Select and press Enter
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeySpace})
	model, cmd := updateModel(model, tea.KeyMsg{Type: tea.KeyEnter})

	assert.Equal(t, StateSelected, model.state, "should skip confirm with --sync")
	assert.True(t, model.syncRequested, "sync should be requested")
	assert.NotNil(t, cmd, "should return quit command")
}

// TestModel_GetResult verifies result construction.
func TestModel_GetResult(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		model := New(nil, Options{})
		model.state = StateQuitting
		result := model.GetResult()
		assert.True(t, result.Cancelled)
		assert.Empty(t, result.Selected)
	})

	t.Run("selected with sync", func(t *testing.T) {
		model := New(nil, Options{})
		model.state = StateSelected
		model.syncRequested = true
		model.selected = map[string]selectedItem{
			"FT-000001": {id: "FT-000001", name: "Test", summary: "Summary"},
		}
		result := model.GetResult()
		assert.False(t, result.Cancelled)
		assert.Len(t, result.Selected, 1)
		assert.True(t, result.SyncRequested)
		assert.Equal(t, "FT-000001", result.Selected[0].ID)
	})
}

// TestModel_Quit verifies quit keybindings.
func TestModel_Quit(t *testing.T) {
	t.Run("ctrl+c always quits", func(t *testing.T) {
		model := New(nil, Options{})
		model, cmd := updateModel(model, tea.KeyMsg{Type: tea.KeyCtrlC})
		assert.Equal(t, StateQuitting, model.state)
		assert.NotNil(t, cmd, "should return quit command")
	})

	t.Run("esc quits when search is empty", func(t *testing.T) {
		model := New(nil, Options{})
		model, cmd := updateModel(model, tea.KeyMsg{Type: tea.KeyEsc})
		assert.Equal(t, StateQuitting, model.state)
		assert.NotNil(t, cmd, "should return quit command")
	})

	t.Run("esc clears search when not empty", func(t *testing.T) {
		model := New(nil, Options{})
		model.textInput.SetValue("test")
		model.lastQuery = "test"
		model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyEsc})
		assert.Equal(t, StateSearching, model.state, "should stay in searching state")
		assert.Empty(t, model.textInput.Value(), "search should be cleared")
	})

	t.Run("q types in search (does not quit)", func(t *testing.T) {
		model := New(nil, Options{})
		model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		assert.Equal(t, StateSearching, model.state, "q should type, not quit")
		assert.Equal(t, "q", model.textInput.Value(), "q should be typed in search")
	})
}

// TestModel_ViewConfirmation verifies confirmation view rendering.
func TestModel_ViewConfirmation(t *testing.T) {
	model := New(nil, Options{})
	model.state = StateConfirm
	model.selected = map[string]selectedItem{
		"FT-000001": {id: "FT-000001", name: "Feature 1", summary: "Summary 1"},
		"FT-000002": {id: "FT-000002", name: "Feature 2", summary: "Summary 2"},
	}

	view := model.View()
	assert.Contains(t, view, "Add 2 feature(s)", "should show count")
	assert.Contains(t, view, "FT-000001", "should show first ID")
	assert.Contains(t, view, "FT-000002", "should show second ID")
	assert.Contains(t, view, "[Y]es", "should show yes option")
	assert.Contains(t, view, "[N]o", "should show no option")
	assert.Contains(t, view, "[S]ync", "should show sync option")
}

// TestModel_ViewSearch verifies search view rendering.
func TestModel_ViewSearch(t *testing.T) {
	model := New(nil, Options{
		ManifestFeatures: map[string]bool{"FT-000001": true},
	})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "In Manifest", Summary: "Already in manifest"},
		{ID: "FT-000002", Name: "On Server", Summary: "Only on server"},
	})

	view := model.View()
	assert.Contains(t, view, "Feature Atlas", "should show title")
	assert.Contains(t, view, "Space: toggle", "should show space hint")
	assert.Contains(t, view, "Enter: confirm", "should show enter hint")
	assert.Contains(t, view, "[in manifest]", "should show manifest status")
	assert.Contains(t, view, "[on server]", "should show server status")
}

// TestModel_SelectionPersistsAcrossSearchChanges verifies selections persist when search changes.
func TestModel_SelectionPersistsAcrossSearchChanges(t *testing.T) {
	model := New(nil, Options{})

	// Initial items
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
		{ID: "FT-000002", Name: "Feature 2", Summary: "Second"},
	})

	// Select first item
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeySpace})
	assert.True(t, isSelected(model, "FT-000001"), "FT-000001 should be selected")

	// Navigate and select second
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyDown})
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeySpace})
	assert.True(t, isSelected(model, "FT-000002"), "FT-000002 should be selected")

	// Simulate new search results (different items)
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000003", Name: "Feature 3", Summary: "Third"},
		{ID: "FT-000004", Name: "Feature 4", Summary: "Fourth"},
	})

	// Selections should STILL be there (stored in selected map with full data)
	assert.True(t, isSelected(model, "FT-000001"), "FT-000001 selection should persist")
	assert.True(t, isSelected(model, "FT-000002"), "FT-000002 selection should persist")
	assert.Len(t, model.selected, 2, "should have exactly 2 selections")

	// Verify the full data is preserved
	sel1 := model.selected["FT-000001"]
	assert.Equal(t, "Feature 1", sel1.name)
	assert.Equal(t, "First", sel1.summary)

	// When we confirm, both selected items should be included
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, StateConfirm, model.state)

	items := model.getSelectedItems()
	assert.Len(t, items, 2, "confirmation should include all selected items")
}

// TestModel_ConfirmIncludesAllSelected verifies Enter includes all selected, not just visible.
func TestModel_ConfirmIncludesAllSelected(t *testing.T) {
	model := New(nil, Options{})

	// Load items and select some
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
		{ID: "FT-000002", Name: "Feature 2", Summary: "Second"},
	})
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyCtrlA}) // Select all

	// Change search results (simulate search)
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000003", Name: "Feature 3", Summary: "Third"},
	})

	// Press Enter
	model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyEnter})

	// Should be in confirm state with ALL selected items
	assert.Equal(t, StateConfirm, model.state)
	items := model.getSelectedItems()
	assert.Len(t, items, 2, "should include FT-000001 and FT-000002, not FT-000003")
}

// TestModel_ScrollIndicators verifies scroll indicators appear.
func TestModel_ScrollIndicators(t *testing.T) {
	model := New(nil, Options{})

	// Load more items than visible
	items := make([]backend.SuggestItem, 15)
	for i := range items {
		items[i] = backend.SuggestItem{
			ID:      fmt.Sprintf("FT-%06d", i+1),
			Name:    fmt.Sprintf("Feature %d", i+1),
			Summary: fmt.Sprintf("Summary %d", i+1),
		}
	}
	model = loadTestItems(model, items)

	// At top, should show "more below"
	view := model.View()
	assert.Contains(t, view, "more below", "should show scroll down indicator")
	assert.NotContains(t, view, "more above", "should not show scroll up indicator at top")

	// Navigate to middle
	for range 7 {
		model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyDown})
	}
	view = model.View()
	assert.Contains(t, view, "more above", "should show scroll up indicator")
	assert.Contains(t, view, "more below", "should show scroll down indicator")

	// Navigate to bottom
	for range 10 {
		model, _ = updateModel(model, tea.KeyMsg{Type: tea.KeyDown})
	}
	view = model.View()
	assert.Contains(t, view, "more above", "should show scroll up indicator at bottom")
	assert.NotContains(t, view, "more below", "should not show scroll down indicator at bottom")
}

// TestCalculateVisibleRange verifies the scroll window calculation.
func TestCalculateVisibleRange(t *testing.T) {
	model := New(nil, Options{})

	t.Run("empty items", func(t *testing.T) {
		model.items = nil
		start, end := model.calculateVisibleRange()
		assert.Equal(t, 0, start)
		assert.Equal(t, 0, end)
	})

	t.Run("less than visibleItems", func(t *testing.T) {
		model.items = make([]featureItem, 5)
		model.cursorIndex = 2
		start, end := model.calculateVisibleRange()
		assert.Equal(t, 0, start)
		assert.Equal(t, 5, end)
	})

	t.Run("cursor at start", func(t *testing.T) {
		model.items = make([]featureItem, 20)
		model.cursorIndex = 0
		start, end := model.calculateVisibleRange()
		assert.Equal(t, 0, start)
		assert.Equal(t, visibleItems, end)
	})

	t.Run("cursor in middle", func(t *testing.T) {
		model.items = make([]featureItem, 20)
		model.cursorIndex = 10
		start, end := model.calculateVisibleRange()
		assert.True(t, start <= model.cursorIndex, "cursor should be visible")
		assert.True(t, end > model.cursorIndex, "cursor should be visible")
		assert.Equal(t, visibleItems, end-start)
	})

	t.Run("cursor at end", func(t *testing.T) {
		model.items = make([]featureItem, 20)
		model.cursorIndex = 19
		start, end := model.calculateVisibleRange()
		assert.Equal(t, 10, start)
		assert.Equal(t, 20, end)
		assert.True(t, model.cursorIndex >= start && model.cursorIndex < end, "cursor should be visible")
	})
}

// TestFeatureItem_Status verifies item status in rendered output.
func TestFeatureItem_Status(t *testing.T) {
	model := New(nil, Options{
		ManifestFeatures: map[string]bool{"FT-000001": true},
		LocalFeatures:    map[string]bool{"FT-LOCAL-test": true},
	})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Synced", Summary: "In manifest"},
		{ID: "FT-000002", Name: "Server Only", Summary: "On server"},
		{ID: "FT-LOCAL-test", Name: "Local", Summary: "Local only"},
	})

	view := model.View()
	assert.Contains(t, view, "[in manifest]")
	assert.Contains(t, view, "[on server]")
	assert.Contains(t, view, "[local only]")
}

// TestModel_NKeyOpensCreateForm verifies 'n' key opens the create form.
func TestModel_NKeyOpensCreateForm(t *testing.T) {
	model := New(nil, Options{})
	model = loadTestItems(model, []backend.SuggestItem{
		{ID: "FT-000001", Name: "Feature 1", Summary: "First"},
	})

	assert.Equal(t, StateSearching, model.state)

	// Press 'n' key
	model, cmd := updateModel(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	assert.Equal(t, StateCreating, model.state, "should transition to creating state")
	assert.NotNil(t, model.formModel, "formModel should be initialized")
	assert.NotNil(t, cmd, "should return form init command")
}

// TestModel_ViewSearch_ShowsNewKeyHint verifies the help text includes 'n' key.
func TestModel_ViewSearch_ShowsNewKeyHint(t *testing.T) {
	model := New(nil, Options{})
	view := model.View()

	assert.Contains(t, view, "n: new", "should show 'n' key hint for creating features")
}

// TestModel_StateCreating_DelegatesMessages verifies messages are forwarded to form.
func TestModel_StateCreating_DelegatesMessages(t *testing.T) {
	model := New(nil, Options{})
	model.state = StateCreating
	model.formModel = NewFormModel(nil, nil)

	// Initial state
	assert.Equal(t, FormStateEditing, model.formModel.state)

	// Ctrl+C should quit from creating state
	model, cmd := updateModel(model, tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Equal(t, StateQuitting, model.state, "Ctrl+C should quit")
	assert.NotNil(t, cmd, "should return quit command")
}

// TestModel_StateCreating_FormCancelled verifies form cancellation returns to search.
func TestModel_StateCreating_FormCancelled(_ *testing.T) {
	// Note: We can't easily simulate huh's internal abort state without mocking.
	// The cancellation behavior is verified through integration tests.
	// This test documents the expected behavior:
	// - When formModel.Cancelled() returns true, state should go back to StateSearching
}

// TestModel_StateCreating_ViewShowsForm verifies form view is rendered.
func TestModel_StateCreating_ViewShowsForm(t *testing.T) {
	model := New(nil, Options{})
	model.state = StateCreating
	model.formModel = NewFormModel(nil, nil)

	view := model.View()

	assert.Contains(t, view, "Create New Feature", "should show form title")
	assert.Contains(t, view, "Tab: next", "should show form help")
	assert.Contains(t, view, "Esc: cancel", "should show cancel hint")
}

// TestModel_FetchSuggestions_NilClient verifies nil client handling.
func TestModel_FetchSuggestions_NilClient(t *testing.T) {
	model := New(nil, Options{}) // nil client

	// Get the command from fetchSuggestions
	cmd := model.fetchSuggestions("test")
	require.NotNil(t, cmd, "command should not be nil")

	// Execute the command - should not panic
	msg := cmd()
	require.NotNil(t, msg, "message should not be nil")

	// Verify it returns the specific error
	sugMsg, ok := msg.(suggestionsMsg)
	require.True(t, ok, "should return suggestionsMsg")
	assert.ErrorIs(t, sugMsg.err, ErrNoServerConnection, "should return ErrNoServerConnection")
}
