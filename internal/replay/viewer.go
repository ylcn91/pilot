package replay

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ViewerModel is the bubbletea model for the interactive replay viewer
type ViewerModel struct {
	recording   *Recording
	events      []*StreamEvent
	current     int
	playing     bool
	speed       float64 // 0.5, 1.0, 2.0, 4.0
	filter      EventFilter
	filteredIdx []int // Indices into events that match filter
	width       int
	height      int
	scrollY     int
	showHelp    bool
	quit        bool
}

// EventFilter controls which events are displayed
type EventFilter struct {
	ShowTools   bool
	ShowText    bool
	ShowResults bool
	ShowSystem  bool
	ShowErrors  bool
}

// DefaultEventFilter returns a filter showing all events
func DefaultEventFilter() EventFilter {
	return EventFilter{
		ShowTools:   true,
		ShowText:    true,
		ShowResults: true,
		ShowSystem:  true,
		ShowErrors:  true,
	}
}

// NewViewerModel creates a new interactive viewer
func NewViewerModel(recording *Recording) (*ViewerModel, error) {
	events, err := LoadStreamEvents(recording)
	if err != nil {
		return nil, fmt.Errorf("failed to load events: %w", err)
	}

	m := &ViewerModel{
		recording: recording,
		events:    events,
		current:   0,
		playing:   false,
		speed:     1.0,
		filter:    DefaultEventFilter(),
		width:     80,
		height:    24,
		scrollY:   0,
		showHelp:  false,
	}

	m.applyFilter()
	return m, nil
}

// Styles
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	currentEventStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("212"))

	eventStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	timestampStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1)

	progressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))
)

// Init implements tea.Model
func (m *ViewerModel) Init() tea.Cmd {
	return nil
}
