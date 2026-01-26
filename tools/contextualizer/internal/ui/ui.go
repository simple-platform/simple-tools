package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"contextualizer/internal/config"
	"contextualizer/internal/processor"
	"os/exec"
	"runtime"
)

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

// Styles
var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575"))
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
	blurredStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	helpStyle = blurredStyle
)

type state int

const (
	stateSelectDirs state = iota
	stateSelectMode
	stateProcessing
	stateDone
)

type Model struct {
	config      *config.Config
	processor   *processor.Processor
	state       state
	cwd         string
	
	// Directory Selection
	availableDirs []string
	selectedDirs  map[string]struct{}
	cursor        int
	
	// Output Mode Selection
	outputModes []string
	modeCursor  int
	outputMode  string
	
	// Processing
	err           error
	
	// Window size
	width, height int
}

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
	
	// Pre-select if configured? For now empty default.
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

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
		// Logic would go here to kick off processing command
		// simplified for this snippet
		return m, nil
	case stateDone:
		return m, tea.Quit
	}

	return m, nil
}

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
			// Determine next state
			m.state = stateSelectMode
		}
	}
	return m, nil
}

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

func (m Model) startProcessingCmd() tea.Msg {
	// This runs in a separate goroutine
    // We iterate over selected dirs and process them
    
    // Ensure output dir exists
    if err := os.MkdirAll(m.config.OutputDir, 0755); err != nil {
        return processingFinishedMsg{err: err}
    }
    
    // Single Mode
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
        // Multiple Mode
        for dir := range m.selectedDirs {
            content, err := m.processor.ProcessDirectory(dir)
            if err != nil {
                return processingFinishedMsg{err: err}
            }
            
            outFile := filepath.Join(m.config.OutputDir, filepath.Base(dir) + ".txt")
            if err := os.WriteFile(outFile, []byte(content), 0644); err != nil {
                 return processingFinishedMsg{err: err}
            }
        }
    }
    
	return processingFinishedMsg{success: true}
}

type processingFinishedMsg struct{
    err error
    success bool
}

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
