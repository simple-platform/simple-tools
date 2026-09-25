package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// deployPlan mirrors the plan `simple deploy` draws, so layout tests see
// the real title widths.
var deployPlan = []Step{
	{ID: "config", Title: "Load project config"},
	{ID: "auth", Title: "Authenticate"},
	{ID: "version", Title: "Bump version"},
	{ID: "collect", Title: "Collect files"},
	{ID: "connect", Title: "Connect"},
	{ID: "manifest", Title: "Compare with server"},
	{ID: "upload", Title: "Upload files"},
	{ID: "publish", Title: "Publish version"},
	{ID: "install", Title: "Install to dev"},
}

const deployHeader = "🚀 Deploying apps/com.acme.crm to dev"

// epoch anchors fixture timelines so durations are exact.
var epoch = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func at(seconds float64) time.Time {
	return epoch.Add(time.Duration(seconds * float64(time.Second)))
}

// newFixtureModel returns a model whose clock starts at epoch.
func newFixtureModel(width, height int) StepsModel {
	m := NewStepsModel(StepsModelConfig{Header: deployHeader, Plan: deployPlan, Width: width, Height: height})
	m.began, m.now = epoch, epoch
	return m
}

// send applies msgs in order and returns the resulting model.
func send(t *testing.T, m StepsModel, msgs ...tea.Msg) StepsModel {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		var ok bool
		if m, ok = next.(StepsModel); !ok {
			t.Fatalf("Update returned %T, want StepsModel", next)
		}
	}
	return m
}

func started(id StepID, sec float64, detail string) tea.Msg {
	return stepStartedMsg{stepEvent{id, at(sec)}, detail}
}

func done(id StepID, sec float64, detail string) tea.Msg {
	return stepDoneMsg{stepEvent{id, at(sec)}, detail}
}

func progressed(id StepID, sec float64, p Progress) tea.Msg {
	return stepProgressMsg{stepEvent{id, at(sec)}, p}
}

// throughManifest is the §3 timeline up to the upload step.
func throughManifest() []tea.Msg {
	return []tea.Msg{
		started("config", 0, ""), done("config", 0.4, "com.acme.crm · tenant acme"),
		started("auth", 0.4, ""), done("auth", 1.3, ""),
		started("version", 1.3, ""), done("version", 1.5, "1.4.3-dev.5 · app.scl updated"),
		started("collect", 1.5, ""), done("collect", 2.1, "496 files · 18.2 MB"),
		started("connect", 2.1, "devops.acme.simple.dev"), done("connect", 2.4, "devops.acme.simple.dev"),
		started("manifest", 2.4, "server is checking 496 files against storage"),
		done("manifest", 43.6, "312 to upload · 184 already on server"),
	}
}

// uploadingModel is the §3 live frame: upload at 65%, 57.3s in.
func uploadingModel(t *testing.T, width, height int) StepsModel {
	t.Helper()
	msgs := append(throughManifest(),
		started("upload", 44.9, ""),
		progressed("upload", 57.3, Progress{Items: 142, ItemsTotal: 312, Bytes: 8_100_000, BytesTotal: 12_400_000, Noun: "files"}),
	)
	return send(t, newFixtureModel(width, height), msgs...)
}

// deployedModel is the §3 success frame.
func deployedModel(t *testing.T, width, height int) StepsModel {
	t.Helper()
	msgs := append(throughManifest(),
		started("upload", 44.9, ""), done("upload", 73.6, "312 files · 12.4 MB"),
		started("publish", 73.6, "server is storing 312 uploaded files"), done("publish", 91.9, "com.acme.crm@1.4.3-dev.5"),
		started("install", 91.9, "running on the server (can take minutes)"), done("install", 203.9, "1.4.3-dev.5"),
		FinishedMsg{},
	)
	return send(t, newFixtureModel(width, height), msgs...)
}

// uploadFailedModel is the §3 failure frame.
func uploadFailedModel(t *testing.T, width, height int) StepsModel {
	t.Helper()
	err := errors.New("upload actions/crm/build/release.wasm: connection to the devops server was lost: websocket: close 1006 (abnormal closure): unexpected EOF")
	msgs := append(throughManifest(),
		started("upload", 44.9, ""),
		stepFailedMsg{stepEvent{"upload", at(49.1)}, err.Error()},
		FinishedMsg{},
	)
	return send(t, newFixtureModel(width, height), msgs...)
}

// installInterruptedModel is the §3 interrupt frame, with a retry note.
func installInterruptedModel(t *testing.T, width, height int) StepsModel {
	t.Helper()
	msgs := append(throughManifest(),
		started("upload", 44.9, ""), done("upload", 73.6, "312 files · 12.4 MB"),
		started("publish", 73.6, ""), done("publish", 91.9, "com.acme.crm@1.4.3-dev.5"),
		started("install", 91.9, "running on the server (can take minutes)"),
		stepNoteMsg{stepEvent{"install", at(95)}, "↻ server resolved 1.4.3-dev.4; retrying install of 1.4.3-dev.5 in 5s (2/4)"},
		InterruptMsg{At: at(140.1)},
	)
	return send(t, newFixtureModel(width, height), msgs...)
}

// plainLines strips styling and splits a frame into lines, dropping the
// empty string after the final newline.
func plainLines(frame string) []string {
	return strings.Split(strings.TrimSuffix(ansi.Strip(frame), "\n"), "\n")
}
