// Package tui provides a terminal user interface for feature-atlas using Bubble Tea.
package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/JoobyPM/feature-atlas-service/internal/apiclient"
)

const (
	debounceDelay  = 300 * time.Millisecond
	maxSuggestions = 10
)

// Styles for the TUI.
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginBottom(1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F87")).
			MarginTop(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			MarginTop(1)
)

// suggestionItem implements list.Item for the suggestion list.
type suggestionItem struct {
	id      string
	name    string
	summary string
}

func (i suggestionItem) Title() string       { return fmt.Sprintf("%s - %s", i.id, i.name) }
func (i suggestionItem) Description() string { return truncate(i.summary, 60) }
func (i suggestionItem) FilterValue() string { return i.id + " " + i.name }

// truncate shortens a string to maxLen runes with ellipsis.
// Uses rune count for proper UTF-8 handling.
func truncate(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen-3]) + "..."
}

// State represents the current UI state.
type State int

// State constants for the TUI lifecycle.
const (
	StateSearching State = iota
	StateSelected
	StateError
	StateQuitting
)

// ErrTUIUnexpectedModel is returned when the TUI returns an unexpected model type.
var ErrTUIUnexpectedModel = errors.New("unexpected TUI model type")

// Model is the Bubble Tea model for the TUI.
type Model struct {
	client     *apiclient.Client
	textInput  textinput.Model
	list       list.Model
	state      State
	selected   *apiclient.SuggestItem
	err        error
	width      int
	height     int
	lastQuery  string
	debounceID int
	loading    bool
}

// debounceMsg is sent after the debounce delay.
type debounceMsg struct {
	query string
	id    int
}

// suggestionsMsg contains the suggestions from the API.
type suggestionsMsg struct {
	items []apiclient.SuggestItem
	err   error
}

// New creates a new TUI model.
func New(client *apiclient.Client) Model {
	ti := textinput.New()
	ti.Placeholder = "Search features..."
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))

	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = selectedStyle
	delegate.Styles.SelectedDesc = dimStyle

	l := list.New([]list.Item{}, delegate, 60, 10)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()

	return Model{
		client:    client,
		textInput: ti,
		list:      l,
		state:     StateSearching,
		width:     80,
		height:    24,
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.fetchSuggestions(""),
	)
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.state = StateQuitting
			return m, tea.Quit

		case "enter":
			if m.state == StateSearching && len(m.list.Items()) > 0 {
				if item, ok := m.list.SelectedItem().(suggestionItem); ok {
					m.selected = &apiclient.SuggestItem{
						ID:      item.id,
						Name:    item.name,
						Summary: item.summary,
					}
					m.state = StateSelected
					return m, tea.Quit
				}
			}

		case "up", "down":
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetWidth(min(msg.Width-4, 80))
		m.list.SetHeight(min(msg.Height-8, 15))

	case debounceMsg:
		if msg.id == m.debounceID {
			return m, m.fetchSuggestions(msg.query)
		}

	case suggestionsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.state = StateError
			return m, nil
		}
		items := make([]list.Item, len(msg.items))
		for i, item := range msg.items {
			items[i] = suggestionItem{
				id:      item.ID,
				name:    item.Name,
				summary: item.Summary,
			}
		}
		m.list.SetItems(items)
		m.err = nil
	}

	// Handle text input
	var tiCmd tea.Cmd
	m.textInput, tiCmd = m.textInput.Update(msg)
	cmds = append(cmds, tiCmd)

	// Debounce search on input change
	newQuery := m.textInput.Value()
	if newQuery != m.lastQuery {
		m.lastQuery = newQuery
		m.debounceID++
		m.loading = true
		cmds = append(cmds, m.debounceSearch(newQuery, m.debounceID))
	}

	return m, tea.Batch(cmds...)
}

// View renders the UI.
func (m Model) View() string {
	if m.state == StateQuitting {
		return ""
	}

	if m.state == StateSelected && m.selected != nil {
		return fmt.Sprintf(
			"%s\n\nSelected: %s\n%s\n%s\n",
			titleStyle.Render("✓ Feature Selected"),
			selectedStyle.Render(m.selected.ID+" - "+m.selected.Name),
			dimStyle.Render(m.selected.Summary),
			helpStyle.Render("Feature ID copied to clipboard conceptually"),
		)
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("🔍 Feature Atlas"))
	b.WriteString("\n\n")
	b.WriteString(m.textInput.View())
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(dimStyle.Render("Searching..."))
		b.WriteString("\n")
	} else if m.err != nil {
		b.WriteString(errorStyle.Render("Error: " + m.err.Error()))
		b.WriteString("\n")
	} else if len(m.list.Items()) == 0 {
		b.WriteString(dimStyle.Render("No results found"))
		b.WriteString("\n")
	} else {
		b.WriteString(m.list.View())
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ navigate • enter select • esc quit"))

	return b.String()
}

// debounceSearch returns a command that triggers a search after a delay.
func (m Model) debounceSearch(query string, id int) tea.Cmd {
	return tea.Tick(debounceDelay, func(_ time.Time) tea.Msg {
		return debounceMsg{query: query, id: id}
	})
}

// fetchSuggestions fetches suggestions from the API.
func (m Model) fetchSuggestions(query string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		items, err := m.client.Suggest(ctx, query, maxSuggestions)
		return suggestionsMsg{items: items, err: err}
	}
}

// Selected returns the selected item, if any.
func (m Model) Selected() *apiclient.SuggestItem {
	return m.selected
}

// Run starts the TUI and returns the selected item.
func Run(client *apiclient.Client) (*apiclient.SuggestItem, error) {
	model := New(client)
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("TUI error: %w", err)
	}

	m, ok := finalModel.(Model)
	if !ok {
		return nil, ErrTUIUnexpectedModel
	}

	return m.Selected(), nil
}
