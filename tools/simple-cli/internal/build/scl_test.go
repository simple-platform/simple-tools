package build

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchSCLParserVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintln(w, `version: "1.2.3"`)
	}))
	defer server.Close()

	origURL := SCLParserMixExsURL
	SCLParserMixExsURL = server.URL
	defer func() { SCLParserMixExsURL = origURL }()

	version, err := fetchSCLParserVersion()
	if err != nil {
		t.Fatalf("fetchSCLParserVersion() error = %v", err)
	}
	if version != "1.2.3" {
		t.Errorf("got version %s, want 1.2.3", version)
	}
}

func TestFetchSCLParserVersion_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	origURL := SCLParserMixExsURL
	SCLParserMixExsURL = server.URL
	defer func() { SCLParserMixExsURL = origURL }()

	_, err := fetchSCLParserVersion()
	if err == nil {
		t.Error("expected error for 404, got nil")
	}
}

func TestExtractVersionFromMixExs(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "valid version",
			content: `def project do [ app: :app, version: "1.2.3", elixir: "~> 1.14" ] end`,
			want:    "1.2.3",
			wantErr: false,
		},
		{
			name:    "version with newlines",
			content: "version:\n \"2.0.0\"",
			// Regex might depend on implementation details, usually \s works for newline too
			want:    "2.0.0",
			wantErr: false,
		},
		{
			name:    "no version",
			content: `def project do [ app: :app ] end`,
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractVersionFromMixExs(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractVersionFromMixExs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractVersionFromMixExs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapSCLPlatform(t *testing.T) {
	tests := []struct {
		platform string
		arch     string
		want     string
	}{
		{"macos", "aarch64", "macos-silicon"},
		{"macos", "x86_64", "macos"},
		{"linux", "aarch64", "linux-arm64"},
		{"linux", "x86_64", "linux"},
		{"windows", "x86_64", "windows.exe"},
		{"unknown", "unknown", "unknown"},
	}

	for _, tt := range tests {
		got := mapSCLPlatform(tt.platform, tt.arch)
		if got != tt.want {
			t.Errorf("mapSCLPlatform(%s, %s) = %s, want %s", tt.platform, tt.arch, got, tt.want)
		}
	}
}

func TestNormalizeActionName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-action", "my_action"},
		{"action_name", "action_name"},
		{"mixed-separators_here", "mixed_separators_here"},
	}

	for _, tt := range tests {
		if got := NormalizeActionName(tt.input); got != tt.want {
			t.Errorf("NormalizeActionName(%s) = %s, want %s", tt.input, got, tt.want)
		}
	}
}
