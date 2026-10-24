package build

import (
	"strings"
	"testing"
)

// THE METADATA PATH READS THE LANGUAGE THROUGH THE FILESYSTEM IT WAS HANDED.
//
// The cases below are the ones this package used to answer with a detector of
// its own that knew TypeScript and Go. They are asked of the detector the
// compiler's path uses, because there is now one of it — a Rust action reached
// the build and did not reach the description, and the reason was that the two
// readers had different lists.
func TestDetectActionLanguageThroughAFileSystem(t *testing.T) {
	tests := []struct {
		name        string
		files       map[string]string
		actionDir   string
		want        ActionLanguage
		wantErr     bool
		errContains string
	}{
		{
			name: "TypeScript action detected",
			files: map[string]string{
				"/action/src/index.ts": "export function handler() {}",
			},
			actionDir: "/action",
			want:      LanguageTypeScript,
		},
		{
			name: "root TypeScript action detected",
			files: map[string]string{
				"/action/index.ts": "export function handler() {}",
			},
			actionDir: "/action",
			want:      LanguageTypeScript,
		},
		{
			name: "Go action detected",
			files: map[string]string{
				"/action/main.go": "package main\n\nfunc main() {}",
			},
			actionDir: "/action",
			want:      LanguageGo,
		},
		{
			name: "Rust action detected",
			files: map[string]string{
				"/action/src/main.rs": "fn main() {}",
				"/action/Cargo.toml":  "[package]\nname = \"greet-user\"\n",
			},
			actionDir: "/action",
			want:      LanguageRust,
		},
		{
			// Both spellings of the TypeScript source are one answer, not two
			// languages, so this is not an ambiguity.
			name: "both TypeScript spellings are one language",
			files: map[string]string{
				"/action/index.ts":     "export function handler() {}",
				"/action/src/index.ts": "export function handler() {}",
			},
			actionDir: "/action",
			want:      LanguageTypeScript,
		},
		{
			name: "ambiguous language - both files present",
			files: map[string]string{
				"/action/src/index.ts": "export function handler() {}",
				"/action/main.go":      "package main\n\nfunc main() {}",
			},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "cannot tell which language action is written in",
		},
		{
			name: "ambiguous language - Rust beside TypeScript",
			files: map[string]string{
				"/action/src/index.ts": "export function handler() {}",
				"/action/src/main.rs":  "fn main() {}",
			},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "src/main.rs (Rust)",
		},
		{
			name:        "missing source file - neither file present",
			files:       map[string]string{},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "no action source found in action",
		},
		{
			name: "missing source file - other files present",
			files: map[string]string{
				"/action/package.json": `{"name": "test"}`,
				"/action/README.md":    "# Test Action",
			},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "expected src/index.ts or index.ts (TypeScript), src/main.rs (Rust), or main.go (Go)",
		},
		{
			name: "TypeScript action with nested directory structure",
			files: map[string]string{
				"/complex/action/src/index.ts": "export function handler() {}",
				"/complex/action/package.json": `{"name": "test"}`,
			},
			actionDir: "/complex/action",
			want:      LanguageTypeScript,
		},
		{
			name: "Go action with nested directory structure",
			files: map[string]string{
				"/complex/action/main.go":   "package main\n\nfunc main() {}",
				"/complex/action/go.mod":    "module test",
				"/complex/action/README.md": "# Test Action",
			},
			actionDir: "/complex/action",
			want:      LanguageGo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &MockFileSystem{files: tt.files}

			got, err := detectActionLanguage(fs, tt.actionDir)

			if tt.wantErr {
				if err == nil {
					t.Errorf("detectActionLanguage() expected error containing '%s', got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("detectActionLanguage() error = %v, want error containing '%s'", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("detectActionLanguage() unexpected error = %v", err)
				return
			}

			if got != tt.want {
				t.Errorf("detectActionLanguage() = %v, want %v", got, tt.want)
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
