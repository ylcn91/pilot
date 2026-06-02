package dashboard

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// GitGraphMode represents the on/off toggle state.
type GitGraphMode int

const (
	GitGraphHidden  GitGraphMode = iota
	GitGraphVisible              // auto-selects Small/Medium/Full based on available width
)

// gitGraphSize is the effective display size, auto-selected from available width.
type gitGraphSize int

const (
	gitGraphSizeSmall  gitGraphSize = iota // graph + message only, title "GIT"
	gitGraphSizeMedium                     // graph + refs + message (no SHA/author)
	gitGraphSizeFull                       // graph + refs + message + SHA + author, title "GIT GRAPH"
)

// gitRefreshMsg is sent after a background git graph refresh completes.
type gitRefreshMsg struct {
	state *GitGraphState
}

// GitGraphState holds the parsed git graph data.
type GitGraphState struct {
	Lines       []GitGraphLine `json:"lines"`
	TotalCount  int            `json:"total_count"`
	Error       string         `json:"error,omitempty"`
	LastRefresh time.Time      `json:"last_refresh"`
}

// GitGraphLine represents one parsed line of git log --graph output.
type GitGraphLine struct {
	GraphChars string `json:"graph_chars"`       // Branch drawing characters (│ ├╌╮ etc.)
	Refs       string `json:"refs,omitempty"`    // Branch/tag decorations
	Message    string `json:"message,omitempty"` // Commit message
	Author     string `json:"author,omitempty"`  // Author name (short)
	SHA        string `json:"sha,omitempty"`     // Short SHA (7 chars)
}

// branchColors cycles per track column (left → right).
// Track 0 (main trunk) is steel blue; others cycle through the palette.
var branchColors = []string{
	"#7eb8da", // steel blue  (track 0 — usually main)
	"#7ec699", // sage green
	"#d4a054", // amber
	"#d48a8a", // dusty rose
	"#8b949e", // mid gray
}

// Graph line styles (initialized once).
var (
	graphMsgStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#c9d1d9"))            // light gray
	graphAuthorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e"))            // mid gray
	graphSHAStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#6e7681"))            // gray dim
	graphBranchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7ec699"))            // sage green
	graphTagStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#d4a054")) // amber bold
	graphHEADStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7eb8da")) // steel bold
	graphScrollStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8b949e"))            // mid gray
)
