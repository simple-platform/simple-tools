package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ProgressMsg struct {
	ID      string
	Message string
	Done    bool
	Error   error
}

type toolState struct {
	name    string
	status  string
	done    bool
	err     error
	spinner spinner.Model
}

type Model struct {
	tools    map[string]*toolState
	keys     []string
	quitting bool
}

func NewModel(toolNames []string) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	tools := make(map[string]*toolState)
	for _, name := range toolNames {
		tools[name] = &toolState{
			name:    name,
			status:  "Waiting...",
			spinner: s,
		}
	}

	return Model{
		tools: tools,
		keys:  toolNames,
	}
}

func (m Model) Init() tea.Cmd {
	return spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmds []tea.Cmd
		for _, key := range m.keys {
			state := m.tools[key]
			if !state.done {
				var cmd tea.Cmd
				state.spinner, cmd = state.spinner.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)
	case ProgressMsg:
		if state, ok := m.tools[msg.ID]; ok {
			state.status = msg.Message
			state.done = msg.Done
			state.err = msg.Error
		}

		allDone := true
		for _, key := range m.keys {
			if !m.tools[key].done {
				allDone = false
				break
			}
		}

		if allDone {
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var s strings.Builder
	s.WriteString("\n  Checking build tools...\n\n")

	for _, key := range m.keys {
		state := m.tools[key]
		if state.done {
			if state.err != nil {
				s.WriteString(fmt.Sprintf("  ❌ %s: %v\n", state.name, state.err))
			} else {
				s.WriteString(fmt.Sprintf("  ✅ %s: %s\n", state.name, state.status))
			}
		} else {
			s.WriteString(fmt.Sprintf("  %s %s: %s\n", state.spinner.View(), state.name, state.status))
		}
	}

	s.WriteString("\n")
	return s.String()
}
