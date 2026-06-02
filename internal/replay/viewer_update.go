package replay

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Update implements tea.Model
func (m *ViewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quit = true
			return m, tea.Quit

		case " ", "p": // Space/P = play/pause toggle
			m.playing = !m.playing
			if m.playing {
				return m, m.tickCmd()
			}

		case "enter", "n": // Next event
			m.playing = false
			m.nextEvent()

		case "N", "shift+n": // Previous event
			m.playing = false
			m.prevEvent()

		case "g": // Go to start
			m.current = 0
			m.updateScroll()

		case "G": // Go to end
			if len(m.filteredIdx) > 0 {
				m.current = len(m.filteredIdx) - 1
				m.updateScroll()
			}

		case "1": // Speed 0.5x
			m.speed = 0.5
		case "2": // Speed 1x
			m.speed = 1.0
		case "3": // Speed 2x
			m.speed = 2.0
		case "4": // Speed 4x
			m.speed = 4.0

		case "t": // Toggle tools
			m.filter.ShowTools = !m.filter.ShowTools
			m.applyFilter()
		case "x": // Toggle text
			m.filter.ShowText = !m.filter.ShowText
			m.applyFilter()
		case "r": // Toggle results
			m.filter.ShowResults = !m.filter.ShowResults
			m.applyFilter()
		case "s": // Toggle system
			m.filter.ShowSystem = !m.filter.ShowSystem
			m.applyFilter()
		case "e": // Toggle errors
			m.filter.ShowErrors = !m.filter.ShowErrors
			m.applyFilter()
		case "a": // Show all
			m.filter = DefaultEventFilter()
			m.applyFilter()

		case "?", "h": // Toggle help
			m.showHelp = !m.showHelp

		case "up", "k":
			m.playing = false
			m.prevEvent()
		case "down", "j":
			m.playing = false
			m.nextEvent()

		case "pgup":
			for i := 0; i < 10; i++ {
				m.prevEvent()
			}
		case "pgdown":
			for i := 0; i < 10; i++ {
				m.nextEvent()
			}
		}

	case tickMsg:
		if m.playing {
			m.nextEvent()
			if m.current >= len(m.filteredIdx)-1 {
				m.playing = false
				return m, nil
			}
			return m, m.tickCmd()
		}
	}

	return m, nil
}

type tickMsg struct{}

func (m *ViewerModel) tickCmd() tea.Cmd {
	delay := time.Duration(float64(200*time.Millisecond) / m.speed)
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m *ViewerModel) nextEvent() {
	if m.current < len(m.filteredIdx)-1 {
		m.current++
		m.updateScroll()
	}
}

func (m *ViewerModel) prevEvent() {
	if m.current > 0 {
		m.current--
		m.updateScroll()
	}
}

func (m *ViewerModel) updateScroll() {
	visibleLines := m.height - 8 // Header + footer
	if m.current < m.scrollY {
		m.scrollY = m.current
	} else if m.current >= m.scrollY+visibleLines {
		m.scrollY = m.current - visibleLines + 1
	}
}

func (m *ViewerModel) applyFilter() {
	m.filteredIdx = nil
	for i, event := range m.events {
		if m.eventMatchesFilter(event) {
			m.filteredIdx = append(m.filteredIdx, i)
		}
	}
	if m.current >= len(m.filteredIdx) {
		m.current = 0
	}
}

func (m *ViewerModel) eventMatchesFilter(event *StreamEvent) bool {
	if event.Parsed == nil {
		return m.filter.ShowSystem
	}

	p := event.Parsed

	// Error filter takes precedence
	if p.IsError && m.filter.ShowErrors {
		return true
	}

	switch p.Type {
	case "system":
		return m.filter.ShowSystem
	case "assistant":
		if p.ToolName != "" {
			return m.filter.ShowTools
		}
		if p.Text != "" {
			return m.filter.ShowText
		}
		return m.filter.ShowSystem
	case "user":
		return m.filter.ShowResults
	case "result":
		if p.IsError {
			return m.filter.ShowErrors
		}
		return m.filter.ShowResults
	default:
		return m.filter.ShowSystem
	}
}
