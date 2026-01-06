package scaffold

import (
	"errors"
	"simple-cli/internal/fsx"
	"strings"
	"testing"
)

// Tests follow...

func TestCreateMonorepoStructure_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mockFS  *fsx.MockFileSystem
		mockTpl *fsx.MockTemplateFS
		wantErr string
	}{
		{
			name: "mkdir failed",
			mockFS: &fsx.MockFileSystem{
				MkdirAllErr: errors.New("mkdir failed"),
			},
			mockTpl: &fsx.MockTemplateFS{},
			wantErr: "failed to create directory apps: mkdir failed",
		},
		{
			name: "write failed",
			mockFS: &fsx.MockFileSystem{
				WriteFileErr: errors.New("write failed"),
			},
			mockTpl: &fsx.MockTemplateFS{},
			wantErr: "write failed",
		},
		{
			name:   "agents copy failed",
			mockFS: &fsx.MockFileSystem{},
			mockTpl: &fsx.MockTemplateFS{
				ReadErrors: map[string]error{
					"templates/AGENTS.md": errors.New("read agents failed"),
				},
			},
			wantErr: "read agents failed",
		},
		{
			name:   "readme render failed",
			mockFS: &fsx.MockFileSystem{},
			mockTpl: &fsx.MockTemplateFS{
				ReadErrors: map[string]error{
					"templates/README.md": errors.New("read readme failed"),
				},
			},
			wantErr: "read readme failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateMonorepoStructure(tt.mockFS, tt.mockTpl, "/path/to/project", "project")
			if err == nil {
				t.Error("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Error = %v, want substring %v", err, tt.wantErr)
			}
		})
	}
}

func TestRenderTemplate_Error(t *testing.T) {
	// Tests that renderTemplate correctly propagates write errors.
	mockFS := &fsx.MockFileSystem{
		WriteFileErr: errors.New("write failed"),
	}
	mockTpl := &fsx.MockTemplateFS{}

	err := renderTemplate(mockFS, mockTpl, "templates/README.md", "README.md", nil)
	if err == nil {
		t.Error("Expected error from renderTemplate write")
	}
}

func TestRenderTemplate_ParseError(t *testing.T) {
	mockFS := &fsx.MockFileSystem{}
	mockTpl := &fsx.MockTemplateFS{
		Files: map[string][]byte{
			"bad.tmpl": []byte("{{ .Unclosed "),
		},
	}

	err := renderTemplate(mockFS, mockTpl, "bad.tmpl", "out", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to parse template") {
		t.Errorf("Expected parse error, got: %v", err)
	}
}

func TestCreateAppStructure_Errors(t *testing.T) {
	tests := []struct {
		name    string
		mockFS  *fsx.MockFileSystem
		mockTpl *fsx.MockTemplateFS
		wantErr string
	}{
		{
			name: "mkdir failed",
			mockFS: &fsx.MockFileSystem{
				MkdirAllErr: errors.New("mkdir failed"),
			},
			mockTpl: &fsx.MockTemplateFS{},
			wantErr: "failed to create app directory: mkdir failed",
		},
		{
			name:   "tables copy failed",
			mockFS: &fsx.MockFileSystem{},
			mockTpl: &fsx.MockTemplateFS{
				ReadErrors: map[string]error{
					"templates/app/tables.scl": errors.New("read tables failed"),
				},
			},
			wantErr: "read tables failed",
		},
		{
			name: "render failed",
			mockFS: &fsx.MockFileSystem{
				WriteFileErr: errors.New("write failed"),
			},
			mockTpl: &fsx.MockTemplateFS{},
			wantErr: "failed to write",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateAppStructure(tt.mockFS, tt.mockTpl, "/root", "com.test", "Test", "Desc")
			if err == nil {
				t.Error("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Error = %v, want substring %v", err, tt.wantErr)
			}
		})
	}
}

func TestPathExists_Error(t *testing.T) {
	// Tests that pathExists returns true for errors other than os.ErrNotExist.
	// This simulates permission issues or other filesystem errors where the path effectively "exists" or blocks creation.
	mockFS := &fsx.MockFileSystem{
		StatErr: errors.New("permission denied"),
	}
	exists := PathExists(mockFS, "/foo")
	if !exists {
		t.Error("pathExists should return true if error is not IsNotExist")
	}
}

func TestTemplateReadError(t *testing.T) {
	mockFS := &fsx.MockFileSystem{}
	mockTpl := &fsx.MockTemplateFS{
		ReadFileErr: errors.New("read failed"),
	}

	// Test copyTemplate read error
	err := copyTemplate(mockFS, mockTpl, "src", "dst")
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Errorf("Expected read error, got: %v", err)
	}

	// Test renderTemplate read error
	err = renderTemplate(mockFS, mockTpl, "src", "dst", nil)
	if err == nil || !strings.Contains(err.Error(), "failed to read template") {
		t.Errorf("Expected read template error, got: %v", err)
	}
}

func TestContextReadDirError(t *testing.T) {
	mockFS := &fsx.MockFileSystem{}
	mockTpl := &fsx.MockTemplateFS{
		ReadDirErr: errors.New("read dir failed"),
	}

	err := copyContextDocs(mockFS, mockTpl, "/root")
	if err == nil || !strings.Contains(err.Error(), "failed to read templates/context") {
		t.Errorf("Expected read dir error, got: %v", err)
	}
}
