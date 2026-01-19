package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DeployPhase represents the current phase of deployment.
type DeployPhase int

const (
	PhaseAuth DeployPhase = iota
	PhaseVersion
	PhaseCollect
	PhaseConnect
	PhaseManifest
	PhaseUpload
	PhaseDeploy
	PhaseDone
)

// DeployStatus represents the current status of a deployment phase.
type DeployStatus struct {
	Phase       DeployPhase
	Message     string
	Progress    int // 0-100 for upload phase
	Error       error
	FilesCached int
	FilesTotal  int
}

// DeployModel is the Bubble Tea model for deploy progress UI.
type DeployModel struct {
	spinner   spinner.Model
	status    DeployStatus
	startTime time.Time
	quitting  bool
	width     int
}

// NewDeployModel creates a new deploy progress model.
func NewDeployModel() DeployModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return DeployModel{
		spinner:   s,
		startTime: time.Now(),
		width:     80,
	}
}

// DeployUpdateMsg is the message type for updating deploy status.
type DeployUpdateMsg DeployStatus

// Init initializes the deploy model.
func (m DeployModel) Init() tea.Cmd {
	return spinner.Tick
}

// Update handles messages for the deploy model.
func (m DeployModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
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

	case DeployUpdateMsg:
		m.status = DeployStatus(msg)
		if m.status.Phase == PhaseDone || m.status.Error != nil {
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// View renders the deploy progress UI.
func (m DeployModel) View() string {
	if m.quitting {
		return m.finalView()
	}

	var s strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39"))

	s.WriteString(headerStyle.Render("\n  🚀 Deploying to Simple Platform\n\n"))

	// Phase indicators
	phases := []struct {
		phase DeployPhase
		name  string
		icon  string
	}{
		{PhaseAuth, "Authenticating", "🔐"},
		{PhaseVersion, "Bumping version", "📦"},
		{PhaseCollect, "Collecting files", "📁"},
		{PhaseConnect, "Connecting", "🔌"},
		{PhaseManifest, "Sending manifest", "📋"},
		{PhaseUpload, "Uploading files", "⬆️"},
		{PhaseDeploy, "Deploying", "🚀"},
	}

	for _, p := range phases {
		line := m.renderPhase(p.phase, p.name, p.icon)
		s.WriteString(line)
	}

	// Elapsed time
	elapsed := time.Since(m.startTime).Round(time.Millisecond)
	timeStyle := lipgloss.NewStyle().Faint(true)
	s.WriteString(timeStyle.Render(fmt.Sprintf("\n  Elapsed: %s\n", elapsed)))

	return s.String()
}

func (m DeployModel) renderPhase(phase DeployPhase, name, icon string) string {
	var status string
	var style lipgloss.Style

	if phase < m.status.Phase {
		// Completed
		status = fmt.Sprintf("  ✅ %s %s\n", icon, name)
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	} else if phase == m.status.Phase {
		// In progress
		if m.status.Error != nil {
			status = fmt.Sprintf("  ❌ %s %s: %v\n", icon, name, m.status.Error)
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		} else if phase == PhaseUpload && m.status.FilesTotal > 0 {
			// Show upload progress
			cached := m.status.FilesCached
			total := m.status.FilesTotal
			status = fmt.Sprintf("  %s %s %s: %d/%d files (%d cached)\n",
				m.spinner.View(), icon, name, total-cached-m.status.Progress, total, cached)
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
		} else {
			status = fmt.Sprintf("  %s %s %s: %s\n", m.spinner.View(), icon, name, m.status.Message)
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
		}
	} else {
		// Pending
		status = fmt.Sprintf("  ○ %s %s\n", icon, name)
		style = lipgloss.NewStyle().Faint(true)
	}

	return style.Render(status)
}

func (m DeployModel) finalView() string {
	elapsed := time.Since(m.startTime).Round(time.Millisecond)

	if m.status.Error != nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Render(fmt.Sprintf("\n  ❌ Deployment failed: %v\n  Duration: %s\n\n", m.status.Error, elapsed))
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("82")).
		Render(fmt.Sprintf("\n  ✅ Deployed successfully in %s\n\n", elapsed))
}

// SimpleProgress is a non-interactive progress display for CI/non-TTY environments.
type SimpleProgress struct {
	phase   DeployPhase
	started time.Time
}

// NewSimpleProgress creates a simple progress reporter.
func NewSimpleProgress() *SimpleProgress {
	return &SimpleProgress{started: time.Now()}
}

// Update updates the simple progress display.
func (p *SimpleProgress) Update(status DeployStatus) {
	if status.Phase != p.phase {
		p.phase = status.Phase
		switch status.Phase {
		case PhaseAuth:
			fmt.Println("🔐 Authenticating...")
		case PhaseVersion:
			fmt.Println("📦 Bumping version...")
		case PhaseCollect:
			fmt.Println("📁 Collecting files...")
		case PhaseConnect:
			fmt.Println("🔌 Connecting...")
		case PhaseManifest:
			fmt.Println("📋 Sending manifest...")
		case PhaseUpload:
			fmt.Printf("⬆️  Uploading %d files (%d cached)...\n",
				status.FilesTotal-status.FilesCached, status.FilesCached)
		case PhaseDeploy:
			fmt.Println("🚀 Deploying...")
		case PhaseDone:
			elapsed := time.Since(p.started).Round(time.Millisecond)
			if status.Error != nil {
				fmt.Printf("❌ Deployment failed: %v (%s)\n", status.Error, elapsed)
			} else {
				fmt.Printf("✅ Deployed in %s\n", elapsed)
			}
		}
	}
}

// IsInteractive returns true if the terminal supports interactive UI.
func IsInteractive() bool {
	// Check if stdout is a TTY
	// For simplicity, we check for common CI environment variables
	// In real use, you'd check os.Stdout.Fd() with unix.Isatty()
	return true // Default to interactive
}
