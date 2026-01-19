package ui

import (
	"strings"
	"testing"
)

func TestNewDeployModel(t *testing.T) {
	m := NewDeployModel()

	if m.spinner.Spinner.Frames == nil {
		t.Error("Spinner not initialized")
	}

	if m.startTime.IsZero() {
		t.Error("Start time not set")
	}
}

func TestDeployModel_View(t *testing.T) {
	m := NewDeployModel()

	view := m.View()

	if !strings.Contains(view, "Deploying to Simple Platform") {
		t.Error("View should contain header")
	}

	if !strings.Contains(view, "Authenticating") {
		t.Error("View should contain authentication phase")
	}

	if !strings.Contains(view, "Elapsed") {
		t.Error("View should contain elapsed time")
	}
}

func TestDeployModel_Update_Status(t *testing.T) {
	m := NewDeployModel()

	// Update to version phase
	newModel, _ := m.Update(DeployUpdateMsg{
		Phase:   PhaseVersion,
		Message: "Bumping to 1.0.1",
	})

	dm := newModel.(DeployModel)
	if dm.status.Phase != PhaseVersion {
		t.Errorf("Phase = %d, want %d", dm.status.Phase, PhaseVersion)
	}
}

func TestDeployModel_Update_Done(t *testing.T) {
	m := NewDeployModel()

	newModel, cmd := m.Update(DeployUpdateMsg{
		Phase: PhaseDone,
	})

	dm := newModel.(DeployModel)
	if !dm.quitting {
		t.Error("Model should be quitting when done")
	}

	// Should return quit command
	if cmd == nil {
		t.Error("Should return quit command when done")
	}
}

func TestDeployModel_Update_Error(t *testing.T) {
	m := NewDeployModel()

	testErr := &mockError{msg: "deployment failed"}
	newModel, _ := m.Update(DeployUpdateMsg{
		Phase: PhaseAuth,
		Error: testErr,
	})

	dm := newModel.(DeployModel)
	if !dm.quitting {
		t.Error("Model should be quitting on error")
	}
}

func TestDeployModel_FinalView_Success(t *testing.T) {
	m := NewDeployModel()
	m.status.Phase = PhaseDone
	m.quitting = true

	view := m.View()

	if !strings.Contains(view, "successfully") {
		t.Errorf("Final view should show success, got: %s", view)
	}
}

func TestDeployModel_FinalView_Error(t *testing.T) {
	m := NewDeployModel()
	m.status.Phase = PhaseAuth
	m.status.Error = &mockError{msg: "auth failed"}
	m.quitting = true

	view := m.View()

	if !strings.Contains(view, "failed") {
		t.Errorf("Final view should show failure, got: %s", view)
	}

	if !strings.Contains(view, "auth failed") {
		t.Errorf("Final view should contain error message, got: %s", view)
	}
}

func TestDeployModel_RenderPhase_Completed(t *testing.T) {
	m := NewDeployModel()
	m.status.Phase = PhaseVersion // Auth is complete

	view := m.View()

	// Auth should show as completed (✅)
	if !strings.Contains(view, "✅") {
		t.Error("Completed phase should show checkmark")
	}
}

func TestDeployModel_RenderPhase_InProgress(t *testing.T) {
	m := NewDeployModel()
	m.status.Phase = PhaseUpload
	m.status.FilesTotal = 10
	m.status.FilesCached = 3

	view := m.View()

	if !strings.Contains(view, "files") {
		t.Error("Upload phase should show file count")
	}
}

func TestSimpleProgress(t *testing.T) {
	p := NewSimpleProgress()

	if p.started.IsZero() {
		t.Error("Start time not set")
	}

	// Just verify it doesn't panic
	p.Update(DeployStatus{Phase: PhaseAuth})
	p.Update(DeployStatus{Phase: PhaseVersion})
	p.Update(DeployStatus{Phase: PhaseCollect})
	p.Update(DeployStatus{Phase: PhaseConnect})
	p.Update(DeployStatus{Phase: PhaseManifest})
	p.Update(DeployStatus{Phase: PhaseUpload, FilesTotal: 5, FilesCached: 2})
	p.Update(DeployStatus{Phase: PhaseDeploy})
	p.Update(DeployStatus{Phase: PhaseDone})
}

func TestSimpleProgress_Error(t *testing.T) {
	p := NewSimpleProgress()

	// Just verify it handles error without panic
	p.Update(DeployStatus{
		Phase: PhaseDone,
		Error: &mockError{msg: "test error"},
	})
}

func TestIsInteractive(t *testing.T) {
	// Should return true by default
	if !IsInteractive() {
		t.Error("IsInteractive should return true by default")
	}
}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}
