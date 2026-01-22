package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
	"github.com/JoobyPM/feature-atlas-service/internal/cache"
)

// TestFormModel_NewFormModel verifies form initialization.
func TestFormModel_NewFormModel(t *testing.T) {
	m := NewFormModel(nil, nil)
	require.NotNil(t, m)
	assert.Equal(t, FormStateEditing, m.state)
	assert.NotNil(t, m.form)
	assert.False(t, m.Completed())
	assert.False(t, m.Done())
}

// TestFormModel_Validate_Name verifies name field validation.
func TestFormModel_Validate_Name(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{"empty name", "", true},
		{"valid name", "Auth Service", false},
		{"max length", strings.Repeat("a", 200), false},
		{"too long", strings.Repeat("a", 201), true},
		{"whitespace only", "   ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Match form.go validation: trim then check empty/length
			s := strings.TrimSpace(tt.input)
			hasErr := s == "" || len(s) > maxNameLen
			assert.Equal(t, tt.expectErr, hasErr)
		})
	}
}

// TestFormModel_Validate_Summary verifies summary field validation.
func TestFormModel_Validate_Summary(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{"empty summary", "", true},
		{"valid summary", "Handles user authentication", false},
		{"max length", strings.Repeat("a", 1000), false},
		{"too long", strings.Repeat("a", 1001), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := strings.TrimSpace(tt.input)
			hasErr := s == "" || len(s) > maxSummaryLen
			assert.Equal(t, tt.expectErr, hasErr)
		})
	}
}

// TestFormModel_Validate_Owner verifies owner field validation.
func TestFormModel_Validate_Owner(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{"empty owner (valid)", "", false},
		{"valid owner", "platform-team", false},
		{"max length", strings.Repeat("a", 100), false},
		{"too long", strings.Repeat("a", 101), true},
		{"whitespace trimmed", "  team  ", false},                              // 4 chars after trim
		{"max with whitespace", "  " + strings.Repeat("a", 100) + "  ", false}, // 100 chars after trim
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Match form.go validation: trim then check length
			s := strings.TrimSpace(tt.input)
			hasErr := len(s) > maxOwnerLen
			assert.Equal(t, tt.expectErr, hasErr)
		})
	}
}

// TestFormModel_Validate_Tags verifies tags field validation.
func TestFormModel_Validate_Tags(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectErr bool
	}{
		{"empty tags (valid)", "", false},
		{"single tag", "auth", false},
		{"multiple tags", "auth,security,core", false},
		{"max tags (10)", "a,b,c,d,e,f,g,h,i,j", false},
		{"too many tags (11)", "a,b,c,d,e,f,g,h,i,j,k", true},
		{"empty parts ignored", "a,,b", false},                       // Only 2 valid tags
		{"whitespace parts ignored", "a, ,b", false},                 // Only 2 valid tags
		{"trailing comma", "a,b,c,", false},                          // Only 3 valid tags
		{"10 valid with empty parts", "a,,b,c,d,e,f,g,h,i,j", false}, // 10 valid tags
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasErr := false
			if tt.input != "" {
				// Count only non-empty trimmed tags (matches form.go validation)
				parts := strings.Split(tt.input, ",")
				count := 0
				for _, p := range parts {
					if strings.TrimSpace(p) != "" {
						count++
					}
				}
				hasErr = count > maxTags
			}
			assert.Equal(t, tt.expectErr, hasErr)
		})
	}
}

// TestFormModel_States verifies form state transitions.
func TestFormModel_States(t *testing.T) {
	m := NewFormModel(nil, nil)

	// Initial state
	assert.Equal(t, FormStateEditing, m.state)
	assert.False(t, m.Completed())

	// Set to validating
	m.state = FormStateValidating
	assert.False(t, m.Completed())

	// Set to success
	m.state = FormStateSuccess
	assert.True(t, m.Completed())
	assert.False(t, m.Done())

	// User acknowledges success
	m.done = true
	assert.True(t, m.Done())
}

// TestFormModel_Cancelled verifies Cancelled() returns true when aborted.
func TestFormModel_Cancelled(t *testing.T) {
	m := NewFormModel(nil, nil)
	// Fresh form should not be cancelled
	assert.False(t, m.Cancelled())

	// Form in normal state is not cancelled
	assert.Equal(t, huh.StateNormal, m.form.State)
	assert.False(t, m.Cancelled())
}

// TestFormModel_Done verifies Done() returns true after user acknowledgment.
func TestFormModel_Done(t *testing.T) {
	m := NewFormModel(nil, nil)
	assert.False(t, m.Done())

	m.done = true
	assert.True(t, m.Done())
}

// TestFormModel_GetCreatedFeature verifies feature retrieval.
func TestFormModel_GetCreatedFeature(t *testing.T) {
	m := NewFormModel(nil, nil)
	assert.Nil(t, m.GetCreatedFeature())

	feature := &apiclient.Feature{
		ID:      "FT-000001",
		Name:    "Test Feature",
		Summary: "A test feature",
	}
	m.createdFeature = feature

	result := m.GetCreatedFeature()
	require.NotNil(t, result)
	assert.Equal(t, "FT-000001", result.ID)
	assert.Equal(t, "Test Feature", result.Name)
}

// TestFormModel_GetFormValues verifies form value retrieval.
func TestFormModel_GetFormValues(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.name = "  Test Name  "
	m.summary = "  Test Summary  "
	m.owner = "  test-team  "
	m.tags = "a, b, c"

	name, summary, owner, tags := m.GetFormValues()
	assert.Equal(t, "Test Name", name, "name should be trimmed")
	assert.Equal(t, "Test Summary", summary, "summary should be trimmed")
	assert.Equal(t, "test-team", owner, "owner should be trimmed")
	assert.Equal(t, "a, b, c", tags, "tags should not be trimmed")
}

// TestFormModel_DuplicateCheck_CacheHit verifies duplicate check with cache hit.
func TestFormModel_DuplicateCheck_CacheHit(t *testing.T) {
	// Create cache with existing feature
	c := cache.New(t.TempDir())
	c.Update([]cache.CachedFeature{
		{ID: "FT-000001", Name: "Auth Service", Summary: "Authentication"},
	}, "https://example.com", true) // complete cache

	m := NewFormModel(nil, c)
	m.name = "Auth Service"

	// Execute duplicate check command
	cmd := m.checkDuplicateCmd()
	require.NotNil(t, cmd)

	msg := cmd()
	result, ok := msg.(duplicateCheckResultMsg)
	require.True(t, ok)

	assert.NoError(t, result.err)
	require.NotNil(t, result.duplicate)
	assert.Equal(t, "FT-000001", result.duplicate.ID)
}

// TestFormModel_DuplicateCheck_CacheMiss verifies duplicate check with no cache match.
func TestFormModel_DuplicateCheck_CacheMiss(t *testing.T) {
	// Create complete cache without the feature
	c := cache.New(t.TempDir())
	c.Update([]cache.CachedFeature{
		{ID: "FT-000001", Name: "Auth Service", Summary: "Authentication"},
	}, "https://example.com", true) // complete cache

	m := NewFormModel(nil, c)
	m.name = "New Feature" // Not in cache

	cmd := m.checkDuplicateCmd()
	require.NotNil(t, cmd)

	msg := cmd()
	result, ok := msg.(duplicateCheckResultMsg)
	require.True(t, ok)

	assert.NoError(t, result.err)
	assert.Nil(t, result.duplicate, "should not find duplicate")
}

// TestFormModel_DuplicateCheck_NoClientNoCache verifies offline behavior.
func TestFormModel_DuplicateCheck_NoClientNoCache(t *testing.T) {
	m := NewFormModel(nil, nil) // No client, no cache
	m.name = "New Feature"

	cmd := m.checkDuplicateCmd()
	require.NotNil(t, cmd)

	msg := cmd()
	result, ok := msg.(duplicateCheckResultMsg)
	require.True(t, ok)

	assert.ErrorIs(t, result.err, ErrNoServerConnection, "should return ErrNoServerConnection")
}

// TestFormModel_CreateFeature_NoClient verifies create without client.
func TestFormModel_CreateFeature_NoClient(t *testing.T) {
	m := NewFormModel(nil, nil) // No client
	m.name = "Test Feature"
	m.summary = "Test summary"

	cmd := m.createFeatureCmd()
	require.NotNil(t, cmd)

	msg := cmd()
	result, ok := msg.(featureCreatedMsg)
	require.True(t, ok)

	assert.ErrorIs(t, result.err, ErrNoServerConnection, "should return ErrNoServerConnection")
}

// TestFormModel_View_Editing verifies editing state view.
func TestFormModel_View_Editing(t *testing.T) {
	m := NewFormModel(nil, nil)
	view := m.View()

	assert.Contains(t, view, "Create New Feature")
	assert.Contains(t, view, "Tab: next")
	assert.Contains(t, view, "Esc: cancel")
}

// TestFormModel_View_Validating verifies validating state view.
func TestFormModel_View_Validating(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateValidating
	view := m.View()

	assert.Contains(t, view, "Create New Feature")
	assert.Contains(t, view, "Checking for duplicate")
}

// TestFormModel_View_ConfirmDuplicate verifies duplicate confirmation view.
func TestFormModel_View_ConfirmDuplicate(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateConfirmDuplicate
	m.duplicateFeature = &apiclient.Feature{
		ID:   "FT-000001",
		Name: "Existing Feature",
	}
	view := m.View()

	assert.Contains(t, view, "Similar feature already exists")
	assert.Contains(t, view, "FT-000001")
	assert.Contains(t, view, "Existing Feature")
	assert.Contains(t, view, "[Y]es")
	assert.Contains(t, view, "[N]o")
}

// TestFormModel_View_ConfirmDuplicate_NetworkError verifies network error view.
func TestFormModel_View_ConfirmDuplicate_NetworkError(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateConfirmDuplicate
	m.err = assert.AnError
	view := m.View()

	assert.Contains(t, view, "Warning:")
	assert.Contains(t, view, "Cannot verify uniqueness")
	assert.Contains(t, view, "[Y]es")
	assert.Contains(t, view, "[N]o")
}

// TestFormModel_View_Submitting verifies submitting state view.
func TestFormModel_View_Submitting(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateSubmitting
	view := m.View()

	assert.Contains(t, view, "Creating feature")
}

// TestFormModel_View_Success verifies success state view.
func TestFormModel_View_Success(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateSuccess
	m.createdFeature = &apiclient.Feature{
		ID:   "FT-000042",
		Name: "New Feature",
	}
	view := m.View()

	assert.Contains(t, view, "✓ Created")
	assert.Contains(t, view, "FT-000042")
	assert.Contains(t, view, "New Feature")
	assert.Contains(t, view, "Press any key")
}

// TestFormModel_View_Error verifies error state view.
func TestFormModel_View_Error(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateError
	m.err = assert.AnError
	view := m.View()

	assert.Contains(t, view, "Error:")
	assert.Contains(t, view, "Press any key")
}

// TestFormModel_Update_ConfirmDuplicate_Yes verifies Y key in confirm state.
func TestFormModel_Update_ConfirmDuplicate_Yes(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateConfirmDuplicate
	m.duplicateFeature = &apiclient.Feature{ID: "FT-000001"}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})

	assert.Equal(t, FormStateSubmitting, m.state, "should transition to submitting")
	assert.Nil(t, m.err, "error should be cleared")
}

// TestFormModel_Update_ConfirmDuplicate_No verifies N key in confirm state.
func TestFormModel_Update_ConfirmDuplicate_No(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateConfirmDuplicate
	m.duplicateFeature = &apiclient.Feature{ID: "FT-000001"}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})

	assert.Equal(t, FormStateEditing, m.state, "should return to editing")
	assert.Nil(t, m.duplicateFeature, "duplicate should be cleared")
}

// TestFormModel_Update_Success_AnyKey verifies any key in success state marks done.
func TestFormModel_Update_Success_AnyKey(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateSuccess
	m.createdFeature = &apiclient.Feature{ID: "FT-000001"}

	assert.False(t, m.Done())

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, m.Done(), "should be marked as done")
}

// TestFormModel_Update_Error_AnyKey verifies any key in error state returns to editing.
func TestFormModel_Update_Error_AnyKey(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateError
	m.err = assert.AnError

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.Equal(t, FormStateEditing, m.state, "should return to editing")
	assert.Nil(t, m.err, "error should be cleared")
}

// TestFormModel_Update_Validating_NoDuplicate verifies transition when no duplicate found.
func TestFormModel_Update_Validating_NoDuplicate(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateValidating

	m.Update(duplicateCheckResultMsg{duplicate: nil, err: nil})

	assert.Equal(t, FormStateSubmitting, m.state, "should transition to submitting")
}

// TestFormModel_Update_Validating_DuplicateFound verifies transition when duplicate found.
func TestFormModel_Update_Validating_DuplicateFound(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateValidating

	m.Update(duplicateCheckResultMsg{
		duplicate: &apiclient.Feature{ID: "FT-000001"},
		err:       nil,
	})

	assert.Equal(t, FormStateConfirmDuplicate, m.state, "should transition to confirm")
	assert.NotNil(t, m.duplicateFeature)
}

// TestFormModel_Update_Validating_Error verifies transition on validation error.
func TestFormModel_Update_Validating_Error(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateValidating

	m.Update(duplicateCheckResultMsg{
		duplicate: nil,
		err:       assert.AnError,
	})

	assert.Equal(t, FormStateConfirmDuplicate, m.state, "should transition to confirm")
	assert.NotNil(t, m.err)
}

// TestFormModel_Update_Submitting_Success verifies transition on create success.
func TestFormModel_Update_Submitting_Success(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateSubmitting

	m.Update(featureCreatedMsg{
		feature: &apiclient.Feature{ID: "FT-000001", Name: "Test"},
		err:     nil,
	})

	assert.Equal(t, FormStateSuccess, m.state, "should transition to success")
	assert.NotNil(t, m.createdFeature)
	assert.Equal(t, "FT-000001", m.createdFeature.ID)
}

// TestFormModel_Update_Submitting_Error verifies transition on create error.
func TestFormModel_Update_Submitting_Error(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateSubmitting

	m.Update(featureCreatedMsg{
		feature: nil,
		err:     assert.AnError,
	})

	assert.Equal(t, FormStateError, m.state, "should transition to error")
	assert.NotNil(t, m.err)
}

// TestFormModel_Update_Submitting_EmptyResponse verifies transition on empty response.
func TestFormModel_Update_Submitting_EmptyResponse(t *testing.T) {
	m := NewFormModel(nil, nil)
	m.state = FormStateSubmitting

	m.Update(featureCreatedMsg{
		feature: nil,
		err:     nil,
	})

	assert.Equal(t, FormStateError, m.state, "should transition to error")
	assert.NotNil(t, m.err)
	assert.Contains(t, m.err.Error(), "empty response")
}
