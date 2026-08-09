package build

import (
	"strings"
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		actionDir   string
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name: "TypeScript action detected",
			files: map[string]string{
				"/action/src/index.ts": "export function handler() {}",
			},
			actionDir: "/action",
			want:      "typescript",
			wantErr:   false,
		},
		{
			name: "root TypeScript action detected",
			files: map[string]string{
				"/action/index.ts": "export function handler() {}",
			},
			actionDir: "/action",
			want:      "typescript",
			wantErr:   false,
		},
		{
			name: "Go action detected",
			files: map[string]string{
				"/action/main.go": "package main\n\nfunc main() {}",
			},
			actionDir: "/action",
			want:      "go",
			wantErr:   false,
		},
		{
			name: "ambiguous language - both files present",
			files: map[string]string{
				"/action/src/index.ts": "export function handler() {}",
				"/action/main.go":      "package main\n\nfunc main() {}",
			},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "ambiguous action language: TypeScript source (index.ts or src/index.ts) and main.go found",
		},
		{
			name:        "missing source file - neither file present",
			files:       map[string]string{},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "no action source file found (expected index.ts, src/index.ts, or main.go)",
		},
		{
			name: "missing source file - other files present",
			files: map[string]string{
				"/action/package.json": `{"name": "test"}`,
				"/action/README.md":    "# Test Action",
			},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "no action source file found (expected index.ts, src/index.ts, or main.go)",
		},
		{
			name: "TypeScript action with nested directory structure",
			files: map[string]string{
				"/complex/action/src/index.ts": "export function handler() {}",
				"/complex/action/package.json": `{"name": "test"}`,
			},
			actionDir: "/complex/action",
			want:      "typescript",
			wantErr:   false,
		},
		{
			name: "Go action with nested directory structure",
			files: map[string]string{
				"/complex/action/main.go":   "package main\n\nfunc main() {}",
				"/complex/action/go.mod":    "module test",
				"/complex/action/README.md": "# Test Action",
			},
			actionDir: "/complex/action",
			want:      "go",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock filesystem with test files
			fs := &MockFileSystem{files: tt.files}

			// Call detectLanguage
			got, err := detectLanguage(fs, tt.actionDir)

			// Check error expectation
			if tt.wantErr {
				if err == nil {
					t.Errorf("detectLanguage() expected error containing '%s', got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("detectLanguage() error = %v, want error containing '%s'", err, tt.errContains)
				}
				return
			}

			// Check for unexpected error
			if err != nil {
				t.Errorf("detectLanguage() unexpected error = %v", err)
				return
			}

			// Validate result
			if got != tt.want {
				t.Errorf("detectLanguage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractMetadata_LanguageDetectionErrors(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		actionDir   string
		wantErr     bool
		errContains string
	}{
		{
			name: "no source files",
			files: map[string]string{
				"/action/package.json": `{"name": "test"}`,
			},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "failed to detect action language",
		},
		{
			name: "ambiguous language",
			files: map[string]string{
				"/action/main.go":      "package main",
				"/action/src/index.ts": "export function handler() {}",
			},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "failed to detect action language",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &MockFileSystem{files: tt.files}

			err := ExtractMetadata(fs, tt.actionDir)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ExtractMetadata() expected error containing '%s', got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ExtractMetadata() error = %v, want error containing '%s'", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ExtractMetadata() unexpected error = %v", err)
			}
		})
	}
}
