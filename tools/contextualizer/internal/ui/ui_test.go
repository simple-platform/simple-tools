package ui

import (
	"contextualizer/internal/config"
	"contextualizer/internal/processor"
	"testing"
	"strings"
	
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModel(t *testing.T) {
	cfg := &config.Config{OutputDir: "out"}
	proc := processor.New(cfg, ".")
	m := NewModel(cfg, proc, ".", []string{"foo", "bar"})
	
	if len(m.availableDirs) != 2 {
		t.Error("Expected 2 available dirs")
	}
	if m.state != stateSelectDirs {
		t.Error("Expected initial state stateSelectDirs")
	}
}

func TestUpdate_SelectDirs(t *testing.T) {
	cfg := &config.Config{OutputDir: "out"}
	m := NewModel(cfg, nil, ".", []string{"dir1", "dir2"})
	
	// Test toggle
	var newM tea.Model
	
	// Space select first
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = newM.(Model)
	if _, ok := m.selectedDirs["dir1"]; !ok {
		t.Error("dir1 should be selected")
	}
	
	// Move down
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = newM.(Model)
	if m.cursor != 1 {
		t.Error("Cursor should be 1")
	}
	
	// Enter without selection (shouldn't advance if nothing selected? Wait, dir1 is selected)
	// Let's unselect dir1 to test empty check
	delete(m.selectedDirs, "dir1")
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(Model)
	if m.state != stateSelectDirs {
		t.Error("Should stay in select dirs if nothing selected")
	}
	
	// Select dir2 and enter
	m.selectedDirs["dir2"] = struct{}{}
	// Should always go to select mode now
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(Model)
	if m.state != stateSelectMode {
		t.Error("Should go to stateSelectMode")
	}
}

func TestUpdate_SelectMode(t *testing.T) {
	cfg := &config.Config{OutputDir: "out"}
	m := NewModel(cfg, nil, ".", []string{"dir1"})
	m.state = stateSelectMode
	
	var cmd tea.Cmd
	var newM tea.Model
	
	// Down
	newM, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = newM.(Model)
	if m.modeCursor != 1 {
		t.Error("Mode cursor should be 1")
	}
	
	// Enter (select single mode)
	if m.outputModes[1] != "single" {
		t.Fatal("Expected mode 1 to be single")
	}
	
	newM, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = newM.(Model)
	
	if m.outputMode != "single" {
		t.Error("Should have set output mode to single")
	}
	if m.state != stateProcessing {
		t.Error("Should go to stateProcessing")
	}
	if cmd == nil {
		t.Error("Should return command to start processing")
	}
}

func TestView(t *testing.T) {
	cfg := &config.Config{OutputDir: "out"}
	m := NewModel(cfg, nil, ".", []string{"dir1"})
	
	v := m.View()
	if !strings.Contains(v, "Select directories") {
		t.Error("View should confirm directory selection state")
	}
}

func TestInit(t *testing.T) {
	m := Model{}
	if m.Init() != nil {
		t.Error("Init should be nil")
	}
}

// Mock processor for integration? 
// Bubbletea commands execute async, so hard to unit test "startProcessingCmd" outcome purely synchronously
// without mocking the processor or refactoring UI further.
// However, we can test that the model handles the finished message correctly.

func TestUpdate_ProcessingFinished(t *testing.T) {
	m := Model{state: stateProcessing}
	// We need to construct the private struct processingFinishedMsg?
	// It's private in ui package. Since we share package `ui`, we can access it provided we are in `ui` package 
	// (Test file declares package ui).
	
	msg := processingFinishedMsg{success: true}
	newM, _ := m.Update(msg)
	m = newM.(Model)
	
	if m.state != stateDone {
		t.Error("Should go to stateDone")
	}
}

func TestUpdate_WindowSize(t *testing.T) {
	m := Model{}
	newM, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	m = newM.(Model)
	if m.width != 100 {
		t.Error("Width not set")
	}
}
