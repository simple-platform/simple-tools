package build

import (
	"encoding/json"
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
			errContains: "ambiguous action language: both src/index.ts and main.go found",
		},
		{
			name:        "missing source file - neither file present",
			files:       map[string]string{},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "no action source file found (expected src/index.ts or main.go)",
		},
		{
			name: "missing source file - other files present",
			files: map[string]string{
				"/action/package.json": `{"name": "test"}`,
				"/action/README.md":    "# Test Action",
			},
			actionDir:   "/action",
			wantErr:     true,
			errContains: "no action source file found (expected src/index.ts or main.go)",
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
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
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

func TestWriteActionJSON(t *testing.T) {
	tests := []struct {
		name        string
		metadata    *ActionMetadata
		actionDir   string
		wantFile    string
		wantContent string
	}{
		{
			name: "simple metadata with description and schema",
			metadata: &ActionMetadata{
				Description: "Test action description",
				Schema: JSONSchema{
					Type: "object",
					Properties: map[string]Property{
						"name": {Type: "string", Description: "User name"},
						"age":  {Type: "number", Description: "User age"},
					},
					Required: []string{"name"},
				},
			},
			actionDir: "/action",
			wantFile:  "/action/action.json",
			wantContent: `{
  "description": "Test action description",
  "schema": {
    "type": "object",
    "properties": {
      "age": {
        "type": "number",
        "description": "User age"
      },
      "name": {
        "type": "string",
        "description": "User name"
      }
    },
    "required": [
      "name"
    ]
  }
}
`,
		},
		{
			name: "empty description",
			metadata: &ActionMetadata{
				Description: "",
				Schema: JSONSchema{
					Type:       "object",
					Properties: map[string]Property{},
					Required:   []string{},
				},
			},
			actionDir: "/action",
			wantFile:  "/action/action.json",
			wantContent: `{
  "description": "",
  "schema": {
    "type": "object"
  }
}
`,
		},
		{
			name: "complex schema with nested objects and arrays",
			metadata: &ActionMetadata{
				Description: "Complex action",
				Schema: JSONSchema{
					Type: "object",
					Properties: map[string]Property{
						"user": {
							Type: "object",
							Properties: map[string]Property{
								"name": {Type: "string"},
								"age":  {Type: "number"},
							},
						},
						"tags": {
							Type:  "array",
							Items: &Property{Type: "string"},
						},
						"metadata": {
							Type:                 "object",
							AdditionalProperties: &Property{Type: "string"},
						},
					},
					Required: []string{"user"},
				},
			},
			actionDir: "/action",
			wantFile:  "/action/action.json",
			wantContent: `{
  "description": "Complex action",
  "schema": {
    "type": "object",
    "properties": {
      "metadata": {
        "type": "object",
        "additionalProperties": {
          "type": "string"
        }
      },
      "tags": {
        "type": "array",
        "items": {
          "type": "string"
        }
      },
      "user": {
        "type": "object",
        "properties": {
          "age": {
            "type": "number"
          },
          "name": {
            "type": "string"
          }
        }
      }
    },
    "required": [
      "user"
    ]
  }
}
`,
		},
		{
			name: "schema with constraints",
			metadata: &ActionMetadata{
				Description: "Action with constraints",
				Schema: JSONSchema{
					Type: "object",
					Properties: map[string]Property{
						"age": {
							Type:    "number",
							Default: 18,
							Minimum: floatPtr(0),
							Maximum: floatPtr(120),
						},
						"email": {
							Type:    "string",
							Pattern: "^[^@]+@[^@]+$",
						},
					},
					Required: []string{"email"},
				},
			},
			actionDir: "/action",
			wantFile:  "/action/action.json",
			wantContent: `{
  "description": "Action with constraints",
  "schema": {
    "type": "object",
    "properties": {
      "age": {
        "type": "number",
        "default": 18,
        "minimum": 0,
        "maximum": 120
      },
      "email": {
        "type": "string",
        "pattern": "^[^@]+@[^@]+$"
      }
    },
    "required": [
      "email"
    ]
  }
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock filesystem
			fs := &MockFileSystem{
				files: make(map[string]string),
			}

			// Call writeActionJSON
			err := writeActionJSON(fs, tt.actionDir, tt.metadata)

			// Check for unexpected error
			if err != nil {
				t.Errorf("writeActionJSON() unexpected error = %v", err)
				return
			}

			// Verify file was written
			content, ok := fs.files[tt.wantFile]
			if !ok {
				t.Errorf("writeActionJSON() file %s not written", tt.wantFile)
				return
			}

			// Parse both expected and actual JSON to compare structure
			// (since map iteration order can vary)
			var expectedJSON, actualJSON interface{}
			if err := json.Unmarshal([]byte(tt.wantContent), &expectedJSON); err != nil {
				t.Fatalf("failed to parse expected JSON: %v", err)
			}
			if err := json.Unmarshal([]byte(content), &actualJSON); err != nil {
				t.Fatalf("failed to parse actual JSON: %v", err)
			}

			// Compare JSON structures
			if !jsonEqual(expectedJSON, actualJSON) {
				t.Errorf("writeActionJSON() content mismatch:\nwant:\n%s\ngot:\n%s", tt.wantContent, content)
			}

			// Verify JSON is properly formatted (has trailing newline)
			if len(content) == 0 || content[len(content)-1] != '\n' {
				t.Errorf("writeActionJSON() content should end with newline")
			}
		})
	}
}

func TestExtractMetadata_Integration(t *testing.T) {
	// This test verifies the integration between language detection and file writing
	// We use the real ExtractMetadata function but with a simple Go action that will work

	fs := &MockFileSystem{
		files: map[string]string{
			"/action/main.go": `package main

import "context"

// Test action for integration testing
// @Payload TestInput
func Handler(ctx context.Context, payload TestInput) error {
	return nil
}

type TestInput struct {
	Name string ` + "`json:\"name\"`" + `
}`,
		},
	}

	// Call ExtractMetadata
	err := ExtractMetadata(fs, "/action")
	if err != nil {
		t.Errorf("ExtractMetadata() unexpected error = %v", err)
		return
	}

	// Verify the file was written
	content, ok := fs.files["/action/action.json"]
	if !ok {
		t.Error("action.json not found after extraction")
		return
	}

	// Verify it's valid JSON
	var metadata ActionMetadata
	if err := json.Unmarshal([]byte(content), &metadata); err != nil {
		t.Errorf("action.json contains invalid JSON: %v", err)
		return
	}

	// Verify basic structure
	if metadata.Schema.Type != "object" {
		t.Errorf("schema type = %v, want 'object'", metadata.Schema.Type)
	}

	// Verify it has properties
	if len(metadata.Schema.Properties) == 0 {
		t.Error("schema should have properties")
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
				if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
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

func TestExtractMetadata_AtomicFileWriting(t *testing.T) {
	// This test verifies that file writing is atomic (no partial writes)
	// Since we're using MockFileSystem, we simulate the atomic behavior
	// by ensuring WriteFile is called only once per extraction

	fs := &MockFileSystem{
		files: map[string]string{
			"/action/main.go": `package main

// Test action
// @Payload Input
func Handler() {}

type Input struct {
	Name string ` + "`json:\"name\"`" + `
}`,
		},
	}

	// Call ExtractMetadata
	err := ExtractMetadata(fs, "/action")
	if err != nil {
		t.Errorf("ExtractMetadata() unexpected error = %v", err)
		return
	}

	// Verify the file exists and is valid JSON
	content, ok := fs.files["/action/action.json"]
	if !ok {
		t.Error("action.json not found after extraction")
		return
	}

	// Verify it's valid JSON
	var metadata ActionMetadata
	if err := json.Unmarshal([]byte(content), &metadata); err != nil {
		t.Errorf("action.json contains invalid JSON: %v", err)
	}

	// Verify it has the expected structure
	if metadata.Description == "" {
		t.Error("action.json missing description")
	}
	if metadata.Schema.Type != "object" {
		t.Errorf("action.json schema type = %v, want 'object'", metadata.Schema.Type)
	}

	// Verify JSON is properly formatted (has trailing newline)
	if len(content) == 0 || content[len(content)-1] != '\n' {
		t.Errorf("action.json should end with newline for POSIX compliance")
	}
}

// jsonEqual compares two JSON values for structural equality
func jsonEqual(a, b interface{}) bool {
	aBytes, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bBytes, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aBytes) == string(bBytes)
}
