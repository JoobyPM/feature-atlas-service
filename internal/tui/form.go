package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/JoobyPM/feature-atlas-service/internal/backend"
	"github.com/JoobyPM/feature-atlas-service/internal/cache"
)

// Key constants.
const (
	keyEsc   = "esc"
	keyCtrlC = "ctrl+c"
)

// FormState tracks the form submission state.
type FormState int

// Form state constants.
const (
	FormStateEditing FormState = iota
	FormStateValidating
	FormStateConfirmDuplicate
	FormStateSubmitting
	FormStateSuccess
	FormStateError
)

// FormModel manages the feature creation form.
// IMPORTANT: Must be used as a pointer (*FormModel) because huh.Form stores
// pointers to the name/summary/owner/tags fields via Value(&m.field).
// Using value semantics would cause dangling pointers.
type FormModel struct {
	form  *huh.Form
	state FormState

	// Form values (bound to huh fields via pointers - must be stable!)
	name    string
	summary string
	owner   string
	tags    string

	// Validation results
	duplicateFeature *backend.Feature

	// Result
	createdFeature *backend.Feature
	err            error
	done           bool // True when user acknowledged success/error and wants to return

	// Dependencies
	backend backend.FeatureBackend
	cache   *cache.Cache
}

// Form field validation limits (match server: handlers.go:183).
const (
	maxNameLen    = 200
	maxSummaryLen = 1000
	maxOwnerLen   = 100
	maxTags       = 10
)

// API query limits.
const (
	duplicateCheckLimit = 50  // Limit for duplicate name search
	cacheRefreshLimit   = 100 // Limit for cache refresh (marks incomplete if hit)
)

// NewFormModel creates a new form model.
// Returns pointer to ensure huh.Form's Value() pointers remain valid.
func NewFormModel(b backend.FeatureBackend, c *cache.Cache) *FormModel {
	m := &FormModel{
		backend: b,
		cache:   c,
		state:   FormStateEditing,
	}
	m.form = m.buildForm()
	return m
}

// Validation errors.
var (
	errNameRequired    = errors.New("name is required")
	errSummaryRequired = errors.New("summary is required")
)

func (m *FormModel) buildForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title("Name").
				Description("Feature name (required)").
				Value(&m.name).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return errNameRequired
					}
					if len(s) > maxNameLen {
						return fmt.Errorf("name too long (max %d)", maxNameLen)
					}
					return nil
				}),

			huh.NewText().
				Key("summary").
				Title("Summary").
				Description("Brief description (required)").
				Value(&m.summary).
				CharLimit(maxSummaryLen).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return errSummaryRequired
					}
					if len(s) > maxSummaryLen {
						return fmt.Errorf("summary too long (max %d)", maxSummaryLen)
					}
					return nil
				}),

			huh.NewInput().
				Key("owner").
				Title("Owner").
				Description("Team or person (optional)").
				Value(&m.owner).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if len(s) > maxOwnerLen {
						return fmt.Errorf("owner too long (max %d)", maxOwnerLen)
					}
					return nil
				}),

			huh.NewInput().
				Key("tags").
				Title("Tags").
				Description("Comma-separated (optional)").
				Value(&m.tags).
				Validate(func(s string) error {
					if s == "" {
						return nil
					}
					// Count only non-empty trimmed tags (ignore empty parts like "a,,b")
					parts := strings.Split(s, ",")
					count := 0
					for _, t := range parts {
						if strings.TrimSpace(t) != "" {
							count++
						}
					}
					if count > maxTags {
						return fmt.Errorf("too many tags (max %d)", maxTags)
					}
					return nil
				}),
		),
	).WithTheme(huh.ThemeCharm())
}

// Init initializes the form.
func (m *FormModel) Init() tea.Cmd {
	return m.form.Init()
}

// Update handles messages for the form.
// Uses pointer receiver to maintain stable memory for huh.Form's Value() pointers.
func (m *FormModel) Update(msg tea.Msg) tea.Cmd {
	switch m.state {
	case FormStateEditing:
		return m.updateEditing(msg)
	case FormStateValidating:
		return m.updateValidating(msg)
	case FormStateConfirmDuplicate:
		return m.updateConfirmDuplicate(msg)
	case FormStateSubmitting:
		return m.updateSubmitting(msg)
	case FormStateSuccess:
		return m.updateSuccess(msg)
	case FormStateError:
		return m.updateError(msg)
	default:
		return nil
	}
}

func (m *FormModel) updateEditing(msg tea.Msg) tea.Cmd {
	// Handle form completion
	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State == huh.StateCompleted {
		// Form submitted - start async validation
		m.state = FormStateValidating
		return m.checkDuplicateCmd()
	}

	return cmd
}

func (m *FormModel) updateValidating(msg tea.Msg) tea.Cmd {
	dupMsg, ok := msg.(duplicateCheckResultMsg)
	if !ok {
		return nil
	}

	if dupMsg.err != nil {
		// Network error - ask user if they want to proceed anyway
		m.err = dupMsg.err
		m.state = FormStateConfirmDuplicate
		return nil
	}
	if dupMsg.duplicate != nil {
		// Found duplicate - ask user to confirm
		m.duplicateFeature = dupMsg.duplicate
		m.state = FormStateConfirmDuplicate
		return nil
	}
	// No duplicate - proceed to create
	m.state = FormStateSubmitting
	return m.createFeatureCmd()
}

func (m *FormModel) updateConfirmDuplicate(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "y", "Y":
			// User confirmed - proceed to create
			m.state = FormStateSubmitting
			m.err = nil
			return m.createFeatureCmd()
		case "n", "N", keyEsc:
			// User cancelled - back to editing
			m.state = FormStateEditing
			m.duplicateFeature = nil
			m.err = nil
			m.form = m.buildForm() // Reset form
			return m.form.Init()
		}
	}
	return nil
}

// Errors for feature creation.
var errEmptyServerResponse = errors.New("server returned empty response")

func (m *FormModel) updateSubmitting(msg tea.Msg) tea.Cmd {
	createdMsg, ok := msg.(featureCreatedMsg)
	if !ok {
		return nil
	}

	if createdMsg.err != nil {
		m.err = createdMsg.err
		m.state = FormStateError
		return nil
	}
	if createdMsg.feature == nil {
		m.err = errEmptyServerResponse
		m.state = FormStateError
		return nil
	}
	m.createdFeature = createdMsg.feature
	m.state = FormStateSuccess
	return nil
}

func (m *FormModel) updateSuccess(msg tea.Msg) tea.Cmd {
	// Any key press marks form as done (TUI will handle transition)
	if _, ok := msg.(tea.KeyMsg); ok {
		m.done = true
	}
	return nil
}

func (m *FormModel) updateError(msg tea.Msg) tea.Cmd {
	// Any key press returns to editing
	if _, ok := msg.(tea.KeyMsg); ok {
		m.state = FormStateEditing
		m.err = nil
		m.form = m.buildForm() // Reset form
		return m.form.Init()
	}
	return nil
}

// View renders the form.
func (m *FormModel) View() string {
	var b strings.Builder

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(colorPrimary)).
		Render("Create New Feature")

	b.WriteString(title)
	b.WriteString("\n\n")

	switch m.state {
	case FormStateEditing:
		b.WriteString(m.form.View())
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Tab: next • Shift+Tab: prev • Enter: submit • Esc: cancel"))

	case FormStateValidating:
		b.WriteString(itemDimStyle.Render("Checking for duplicate names..."))

	case FormStateConfirmDuplicate:
		if m.err != nil {
			b.WriteString(errorStyle.Render(fmt.Sprintf("Warning: %v", m.err)))
			b.WriteString("\n\n")
			b.WriteString("Cannot verify uniqueness. Create anyway?\n\n")
		} else if m.duplicateFeature != nil {
			b.WriteString(errorStyle.Render("Similar feature already exists:"))
			b.WriteString("\n\n")
			b.WriteString(fmt.Sprintf("  %s - %s\n", m.duplicateFeature.ID, m.duplicateFeature.Name))
			b.WriteString("\n")
			b.WriteString("Create anyway?\n\n")
		}
		b.WriteString(helpStyle.Render("[Y]es  [N]o"))

	case FormStateSubmitting:
		if m.backend != nil && m.backend.Mode() == "gitlab" {
			b.WriteString(itemDimStyle.Render("Creating Merge Request..."))
		} else {
			b.WriteString(itemDimStyle.Render("Creating feature..."))
		}

	case FormStateSuccess:
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorGreen)).
			Render(fmt.Sprintf("✓ Created %s - %s", m.createdFeature.ID, m.createdFeature.Name)))
		b.WriteString("\n")
		// Show mode-specific guidance
		if m.backend != nil && m.backend.Mode() == "gitlab" {
			b.WriteString(itemDimStyle.Render("  → Merge Request created - feature will appear after MR is merged"))
			b.WriteString("\n")
		}
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Press any key to continue"))

	case FormStateError:
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Press any key to go back"))
	}

	return b.String()
}

// Completed returns true if form is done (success).
func (m *FormModel) Completed() bool {
	return m.state == FormStateSuccess
}

// Cancelled returns true if user cancelled (huh handles Esc → StateAborted).
func (m *FormModel) Cancelled() bool {
	return m.form.State == huh.StateAborted
}

// Done returns true if user has acknowledged success and wants to return.
func (m *FormModel) Done() bool {
	return m.done
}

// GetCreatedFeature returns the created feature (if any).
func (m *FormModel) GetCreatedFeature() *backend.Feature {
	return m.createdFeature
}

// GetFormValues returns the current form values.
func (m *FormModel) GetFormValues() (name, summary, owner, tags string) {
	return strings.TrimSpace(m.name), strings.TrimSpace(m.summary),
		strings.TrimSpace(m.owner), m.tags
}

// Messages for async operations.
type duplicateCheckResultMsg struct {
	duplicate *backend.Feature
	err       error
}

type featureCreatedMsg struct {
	feature *backend.Feature
	err     error
}

// checkDuplicateCmd checks for duplicate feature name.
// Captures values needed for the async operation (safe to read from pointer).
func (m *FormModel) checkDuplicateCmd() tea.Cmd {
	// Capture values for closure (don't capture pointer to avoid races)
	name := strings.TrimSpace(m.name)
	backendRef := m.backend
	cacheRef := m.cache

	return func() tea.Msg {
		// 1. Quick cache check (only if cache is fresh and complete)
		if cacheRef != nil && !cacheRef.IsStale() && cacheRef.IsComplete() {
			if cached := cacheRef.FindByNameExact(name); cached != nil {
				// Cache is authoritative - return duplicate without server call
				return duplicateCheckResultMsg{
					duplicate: &backend.Feature{
						ID:   cached.ID,
						Name: cached.Name,
					},
				}
			}
			// Cache is complete and fresh, name not found = no duplicate
			return duplicateCheckResultMsg{}
		}

		// 2. Backend check (authoritative when cache is stale/incomplete)
		if backendRef != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Search for features with similar name
			results, err := backendRef.Search(ctx, name, duplicateCheckLimit)
			if err != nil {
				return duplicateCheckResultMsg{err: err}
			}

			// Client-side exact match (server uses Contains)
			for i := range results {
				if strings.EqualFold(results[i].Name, name) {
					return duplicateCheckResultMsg{duplicate: &results[i]}
				}
			}
			return duplicateCheckResultMsg{} // Backend check passed, no duplicate
		}

		// 3. Offline and no usable cache - return error so user is warned
		return duplicateCheckResultMsg{
			err: fmt.Errorf("%w: cannot verify name uniqueness", ErrNoServerConnection),
		}
	}
}

// createFeatureCmd creates the feature on the backend.
// Captures values needed for the async operation.
func (m *FormModel) createFeatureCmd() tea.Cmd {
	// Capture values for closure
	backendRef := m.backend
	name := strings.TrimSpace(m.name)
	summary := strings.TrimSpace(m.summary)
	owner := strings.TrimSpace(m.owner)
	tagsStr := m.tags

	return func() tea.Msg {
		if backendRef == nil {
			return featureCreatedMsg{err: fmt.Errorf("%w: cannot create feature", ErrNoServerConnection)}
		}

		// Parse tags
		var tags []string
		if tagsStr != "" {
			parts := strings.Split(tagsStr, ",")
			tags = make([]string, 0, len(parts))
			for _, t := range parts {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
		}

		// GitLab MR creation can take time - use 60s timeout
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		feature := backend.Feature{
			Name:    name,
			Summary: summary,
			Owner:   owner,
			Tags:    tags,
		}

		created, err := backendRef.CreateFeature(ctx, feature)
		if err != nil {
			return featureCreatedMsg{err: err}
		}

		return featureCreatedMsg{feature: created}
	}
}
