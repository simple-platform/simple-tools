package build

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsureTool_ProgressReporting(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		// Write 100 bytes
		data := make([]byte, 100)
		_, _ = w.Write(data)
	}))
	defer server.Close()

	var statuses []string
	def := ToolDef{
		Name: "progress-tool",
		CheckVersionFn: func() (string, error) {
			return "1.0.0", nil
		},
		DownloadURLFn: func(version string) string {
			return server.URL
		},
		OnStatus: func(status string) {
			statuses = append(statuses, status)
		},
	}

	_, err := EnsureTool(def)
	if err != nil {
		t.Fatalf("EnsureTool() error = %v", err)
	}

	if len(statuses) == 0 {
		t.Error("OnStatus not called")
	}

	foundPercent := false
	for _, s := range statuses {
		if strings.Contains(s, "%") {
			foundPercent = true
			break
		}
	}
	if !foundPercent {
		t.Errorf("No percentage reported in statuses: %v", statuses)
	}

	// Check specifically for "100%"
	found100 := false
	for _, s := range statuses {
		if strings.Contains(s, "100%") {
			found100 = true
			break
		}
	}
	if !found100 {
		t.Errorf("No 100%% reported in statuses: %v", statuses)
	}
}
