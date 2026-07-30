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
	name   string
	status string
	done   bool
	err    error
}

// Rows the view spends on chrome: leading blank, header, blank, trailing blank.
const progressChromeRows = 4

type Model struct {
	tools    map[string]*toolState
	keys     []string
	quitting bool
	width    int
	height   int
	// One spinner drives every in-progress row. Per-row spinners meant one tick
	// loop per tool, so a build with 17 targets repainted the whole frame 17
	// times per interval instead of once.
	spinner spinner.Model
}

func NewModel(toolNames []string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	tools := make(map[string]*toolState)
	for _, name := range toolNames {
		tools[name] = &toolState{
			name:   name,
			status: "Waiting...",
		}
	}

	return Model{
		tools:   tools,
		keys:    toolNames,
		width:   80,
		height:  24,
		spinner: sp,
	}
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
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

// truncate clips a line to w display cells so it can never wrap. A wrapped line
// occupies two terminal rows while the renderer accounts for one, which is what
// makes the whole frame smear and duplicate on redraw.
func truncate(line string, w int) string {
	if w <= 0 {
		return line
	}
	runes := []rune(line)
	if len(runes) <= w {
		return line
	}
	if w <= 1 {
		return string(runes[:w])
	}
	return string(runes[:w-1]) + "…"
}

// visibleKeys picks which rows to draw when the tool list is taller than the
// terminal. Unfinished work and failures are what the user is waiting on, so
// they win the available rows; the rest collapse into a single summary line.
func (m Model) visibleKeys(rows int) (keys []string, hiddenDone, hiddenPending int) {
	if rows >= len(m.keys) {
		return m.keys, 0, 0
	}
	if rows < 1 {
		rows = 1
	}

	// Reserve the last row for the summary line.
	budget := rows - 1
	if budget < 1 {
		budget = 1
	}

	shown := make(map[string]bool, budget)
	for _, key := range m.keys {
		if len(shown) == budget {
			break
		}
		if state := m.tools[key]; !state.done || state.err != nil {
			shown[key] = true
		}
	}
	for _, key := range m.keys {
		if len(shown) == budget {
			break
		}
		shown[key] = true
	}

	for _, key := range m.keys {
		if shown[key] {
			keys = append(keys, key)
			continue
		}
		if m.tools[key].done {
			hiddenDone++
		} else {
			hiddenPending++
		}
	}
	return keys, hiddenDone, hiddenPending
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	rows := m.height - progressChromeRows
	keys, hiddenDone, hiddenPending := m.visibleKeys(rows)

	var s strings.Builder
	s.WriteString("\n  Checking build tools...\n\n")

	for _, key := range keys {
		state := m.tools[key]
		// Pad status so a shorter status fully overwrites a longer previous one
		// (e.g. "Done" replacing "Optimizing (Sync)...").
		paddedStatus := fmt.Sprintf("%-30s", state.status)
		var line string
		if state.done {
			if state.err != nil {
				line = fmt.Sprintf("  ❌ %s: %v", state.name, state.err)
			} else {
				line = fmt.Sprintf("  ✅ %s: %s", state.name, paddedStatus)
			}
		} else {
			line = fmt.Sprintf("  %s %s: %s", m.spinner.View(), state.name, paddedStatus)
		}
		s.WriteString(truncate(line, m.width))
		s.WriteString("\n")
	}

	if hiddenDone+hiddenPending > 0 {
		summary := fmt.Sprintf("  … %d more (%d done, %d pending)",
			hiddenDone+hiddenPending, hiddenDone, hiddenPending)
		s.WriteString(truncate(summary, m.width))
		s.WriteString("\n")
	}

	s.WriteString("\n")
	return s.String()
}
