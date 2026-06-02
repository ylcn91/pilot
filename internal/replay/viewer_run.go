package replay

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// RunViewer starts the interactive TUI viewer
func RunViewer(recording *Recording) error {
	model, err := NewViewerModel(recording)
	if err != nil {
		return err
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// RunViewerWithOptions starts the viewer with custom options
func RunViewerWithOptions(recording *Recording, startAt int, filter EventFilter) error {
	model, err := NewViewerModel(recording)
	if err != nil {
		return err
	}

	model.filter = filter
	model.applyFilter()

	// Find the filtered index for startAt
	for i, idx := range model.filteredIdx {
		if idx >= startAt {
			model.current = i
			break
		}
	}
	model.updateScroll()

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// CheckTerminalSupport checks if the terminal supports interactive mode
func CheckTerminalSupport() bool {
	// Check if we're running in a TTY
	if fi, _ := os.Stdout.Stat(); (fi.Mode() & os.ModeCharDevice) == 0 {
		return false
	}
	return true
}
