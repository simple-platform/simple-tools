// Package ui implements the terminal user interface (TUI) for the Contextualizer.
// It uses the Bubble Tea framework to manage state and render the interactive interface.
package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"contextualizer/internal/config"
	"contextualizer/internal/processor"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openDir opens the specified directory in the system's default file explorer.
func openDir(path string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux"
		cmd = "xdg-open"
	}

	args = append(args, path)
	_ = exec.Command(cmd, args...).Run()
}

// Styles definitions for consistent UI appearance.
var (
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575"))
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	blurredStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cursorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	helpStyle         = blurredStyle
)

// state represents the different phases of the TUI application lifecycle.
type state int

const (
	stateSelectDirs state = iota // User is selecting directories to process
	stateSelectMode              // User is selecting the output format
	stateProcessing              // Application is generating context files
	stateDone                    // Processing complete
)

// Model holds the TUI state and application configuration.
type Model struct {
	config    *config.Config
	processor *processor.Processor
	state     state
	cwd       string

	// Directory Selection state
	availableDirs []string
	selectedDirs  map[string]struct{}
	cursor        int

	// Output Mode Selection state
	outputModes []string
	modeCursor  int
	outputMode  string

	// Processing state details
	err error

	// Terminal dimensions
	width, height int
}

// NewModel creates an initial TUI model with default settings.
func NewModel(cfg *config.Config, proc *processor.Processor, wd string, subDirs []string) Model {
	m := Model{
		config:        cfg,
		processor:     proc,
		state:         stateSelectDirs,
		cwd:           wd,
		availableDirs: subDirs,
		selectedDirs:  make(map[string]struct{}),
		outputModes:   []string{"multiple", "single"},
	}

	return m
}

// Init handles any initial I/O setup. None needed here.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles incoming messages (keypresses, window resizes, etc.) and transitions state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case processingFinishedMsg:
		if msg.err != nil {
			fmt.Printf("Error: %v\n", msg.err)
			return m, tea.Quit
		}

		fmt.Printf("Done! Generated context in %s\n", m.config.OutputDir)
		if m.config.OpenOutputDirectory {
			openDir(m.config.OutputDir)
		}
		return m, tea.Quit
	}

	switch m.state {
	case stateSelectDirs:
		return m.updateSelectDirs(msg)
	case stateSelectMode:
		return m.updateSelectMode(msg)
	case stateProcessing:
		// Logic handles transition via cmds, no user input handling needed here except quit
		return m, nil
	case stateDone:
		return m, tea.Quit
	}

	return m, nil
}

// updateSelectDirs handles input during directory selection phase.
func (m Model) updateSelectDirs(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.availableDirs)-1 {
				m.cursor++
			}
		case " ":
			dir := m.availableDirs[m.cursor]
			if _, ok := m.selectedDirs[dir]; ok {
				delete(m.selectedDirs, dir)
			} else {
				m.selectedDirs[dir] = struct{}{}
			}
		case "enter":
			if len(m.selectedDirs) == 0 {
				return m, nil // Must select at least one
			}
			// Transition to mode selection
			m.state = stateSelectMode
		}
	}
	return m, nil
}

// updateSelectMode handles input during output mode selection.
func (m Model) updateSelectMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.modeCursor > 0 {
				m.modeCursor--
			}
		case "down", "j":
			if m.modeCursor < len(m.outputModes)-1 {
				m.modeCursor++
			}
		case "enter":
			m.outputMode = m.outputModes[m.modeCursor]
			m.state = stateProcessing
			return m, m.startProcessingCmd
		}
	}
	return m, nil
}

// startProcessingCmd triggers the async processing logic.
func (m Model) startProcessingCmd() tea.Msg {
	// Processing happens in a separate goroutine implicitly via Bubble Tea command pattern.

	// Clear output dir to avoid stale files
	if err := os.RemoveAll(m.config.OutputDir); err != nil {
		return processingFinishedMsg{err: err}
	}

	// Re-create output dir
	if err := os.MkdirAll(m.config.OutputDir, 0755); err != nil {
		return processingFinishedMsg{err: err}
	}

	// Single Mode: Combine all contents into one file
	if m.outputMode == "single" {
		var combined strings.Builder
		for dir := range m.selectedDirs {
			content, err := m.processor.ProcessDirectory(dir)
			if err != nil {
				return processingFinishedMsg{err: err}
			}
			combined.WriteString(fmt.Sprintf("\n\n# Project: %s\n\n", filepath.Base(dir)))
			combined.WriteString(content)
		}

		outFile := filepath.Join(m.config.OutputDir, "context.txt")
		if err := os.WriteFile(outFile, []byte(combined.String()), 0644); err != nil {
			return processingFinishedMsg{err: err}
		}
	} else {
		// Multiple Mode: Create a file for each selected directory
		for dir := range m.selectedDirs {
			content, err := m.processor.ProcessDirectory(dir)
			if err != nil {
				return processingFinishedMsg{err: err}
			}

			outFile := filepath.Join(m.config.OutputDir, filepath.Base(dir)+".txt")
			if err := os.WriteFile(outFile, []byte(content), 0644); err != nil {
				return processingFinishedMsg{err: err}
			}
		}
	}

	return processingFinishedMsg{success: true}
}

// processingFinishedMsg signals completion of the background task.
type processingFinishedMsg struct {
	err     error
	success bool
}

// View directs the rendering logic based on the current state.
func (m Model) View() string {
	var s strings.Builder

	s.WriteString(titleStyle.Render("Contextualizer") + "\n\n")

	switch m.state {
	case stateSelectDirs:
		s.WriteString("Select directories to process:\n\n")
		for i, dir := range m.availableDirs {
			cursor := " "
			if m.cursor == i {
				cursor = cursorStyle.Render(">")
			}
			checked := "[ ]"
			if _, ok := m.selectedDirs[dir]; ok {
				checked = selectedItemStyle.Render("[x]")
			}
			displayDir := dir
			if rel, err := filepath.Rel(m.cwd, dir); err == nil {
				displayDir = rel
			}
			s.WriteString(fmt.Sprintf("%s %s %s\n", cursor, checked, displayDir))
		}
		s.WriteString(helpStyle.Render("\n(space to toggle, enter to confirm, q to quit)"))

	case stateSelectMode:
		s.WriteString("Select output mode:\n\n")
		for i, mode := range m.outputModes {
			cursor := " "
			if m.modeCursor == i {
				cursor = cursorStyle.Render(">")
			}
			s.WriteString(fmt.Sprintf("%s %s\n", cursor, mode))
		}

	case stateProcessing:
		s.WriteString("Processing...\n")

	case stateDone:
		if m.err != nil {
			s.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(fmt.Sprintf("Error: %v", m.err)))
		} else {
			s.WriteString("Done! Check " + m.config.OutputDir + "\n")
		}
		s.WriteString("\n(q to quit)")
	}

	return s.String()
}

// Update loop needs to handle the finished msg
