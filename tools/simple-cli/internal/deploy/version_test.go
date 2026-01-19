package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

// MockFileSystem implements FileSystem for testing.
type MockFileSystem struct {
	Files      map[string][]byte
	ReadErr    error
	WriteErr   error
	StatErr    error
	WriteCalls []WriteCall
}

type WriteCall struct {
	Path string
	Data []byte
	Perm os.FileMode
}

func (m *MockFileSystem) ReadFile(path string) ([]byte, error) {
	if m.ReadErr != nil {
		return nil, m.ReadErr
	}
	if data, ok := m.Files[path]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (m *MockFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	if m.WriteErr != nil {
		return m.WriteErr
	}
	m.WriteCalls = append(m.WriteCalls, WriteCall{Path: path, Data: data, Perm: perm})
	m.Files[path] = data
	return nil
}

func (m *MockFileSystem) Stat(path string) (os.FileInfo, error) {
	if m.StatErr != nil {
		return nil, m.StatErr
	}
	if _, ok := m.Files[path]; ok {
		return nil, nil // Simplified - just return nil for exists
	}
	return nil, os.ErrNotExist
}

// MockSCLParser implements SCLParser for testing.
type MockSCLParser struct {
	Result []SCLBlock
	Err    error
}

func (m *MockSCLParser) Parse(_ string) ([]SCLBlock, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Result, nil
}

func TestComputeNewVersion(t *testing.T) {
	tests := []struct {
		name        string
		current     string
		env         string
		bump        string
		expected    string
		expectError bool
		errContains string
	}{
		// Prod → Non-prod (requires --bump)
		{"prod to dev with patch", "1.0.0", "dev", "patch", "1.0.1-dev.1", false, ""},
		{"prod to dev with minor", "1.0.0", "dev", "minor", "1.1.0-dev.1", false, ""},
		{"prod to dev with major", "1.0.0", "dev", "major", "2.0.0-dev.1", false, ""},
		{"prod to dev no bump", "1.0.0", "dev", "", "", true, "--bump required"},
		{"prod to staging with patch", "2.5.0", "staging", "patch", "2.5.1-staging.1", false, ""},

		// Non-prod → Same env (auto-increment)
		{"dev to dev", "1.0.1-dev.1", "dev", "", "1.0.1-dev.2", false, ""},
		{"dev to dev high counter", "1.0.1-dev.99", "dev", "", "1.0.1-dev.100", false, ""},
		{"staging to staging", "1.0.1-staging.5", "staging", "", "1.0.1-staging.6", false, ""},
		{"qa to qa", "2.0.0-qa.10", "qa", "", "2.0.0-qa.11", false, ""},

		// Non-prod → Different env (reset counter)
		{"dev to staging", "1.0.1-dev.5", "staging", "", "1.0.1-staging.1", false, ""},
		{"staging to dev", "1.0.1-staging.3", "dev", "", "1.0.1-dev.1", false, ""},
		{"dev to qa", "1.0.1-dev.10", "qa", "", "1.0.1-qa.1", false, ""},

		// Non-prod → Prod (strip prerelease)
		{"dev to prod", "1.0.1-dev.5", "prod", "", "1.0.1", false, ""},
		{"staging to prod", "1.0.1-staging.3", "prod", "", "1.0.1", false, ""},
		{"qa to prod", "2.0.0-qa.15", "prod", "", "2.0.0", false, ""},

		// Prod → Prod (requires --bump)
		{"prod to prod patch", "1.0.0", "prod", "patch", "1.0.1", false, ""},
		{"prod to prod minor", "1.0.0", "prod", "minor", "1.1.0", false, ""},
		{"prod to prod major", "1.0.0", "prod", "major", "2.0.0", false, ""},
		{"prod to prod no bump", "1.0.0", "prod", "", "", true, "--bump required"},

		// Edge cases
		{"invalid bump type", "1.0.0", "dev", "invalid", "", true, "invalid bump type"},
		{"zero version", "0.0.0", "dev", "patch", "0.0.1-dev.1", false, ""},
		{"custom env", "1.0.0-qa.5", "qa", "", "1.0.0-qa.6", false, ""},
		{"large version numbers", "99.99.99", "dev", "patch", "99.99.100-dev.1", false, ""},
		{"prerelease counter at 1", "1.0.0-dev.1", "dev", "", "1.0.0-dev.2", false, ""},

		// Complex prerelease handling
		{"prod to prod after dev cycle", "1.0.1-dev.10", "prod", "", "1.0.1", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ComputeNewVersion(tt.current, tt.env, tt.bump)

			if tt.expectError {
				if err == nil {
					t.Errorf("ComputeNewVersion(%q, %q, %q) expected error, got nil",
						tt.current, tt.env, tt.bump)
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("ComputeNewVersion() error = %v, want containing %q",
						err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ComputeNewVersion(%q, %q, %q) unexpected error = %v",
					tt.current, tt.env, tt.bump, err)
				return
			}

			if result != tt.expected {
				t.Errorf("ComputeNewVersion(%q, %q, %q) = %q, want %q",
					tt.current, tt.env, tt.bump, result, tt.expected)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		wantMajor      int
		wantMinor      int
		wantPatch      int
		wantPrerelease string
	}{
		{"simple version", "1.2.3", 1, 2, 3, ""},
		{"with prerelease", "1.2.3-dev.5", 1, 2, 3, "dev.5"},
		{"zero version", "0.0.0", 0, 0, 0, ""},
		{"large numbers", "99.88.77", 99, 88, 77, ""},
		{"staging prerelease", "1.0.0-staging.10", 1, 0, 0, "staging.10"},
		{"complex prerelease", "1.0.0-alpha.beta.1", 1, 0, 0, "alpha.beta.1"},
		{"empty string", "", 0, 0, 0, ""},
		{"partial version", "1.2", 0, 0, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, patch, prerelease := ParseVersion(tt.version)

			if major != tt.wantMajor {
				t.Errorf("ParseVersion(%q) major = %d, want %d", tt.version, major, tt.wantMajor)
			}
			if minor != tt.wantMinor {
				t.Errorf("ParseVersion(%q) minor = %d, want %d", tt.version, minor, tt.wantMinor)
			}
			if patch != tt.wantPatch {
				t.Errorf("ParseVersion(%q) patch = %d, want %d", tt.version, patch, tt.wantPatch)
			}
			if prerelease != tt.wantPrerelease {
				t.Errorf("ParseVersion(%q) prerelease = %q, want %q", tt.version, prerelease, tt.wantPrerelease)
			}
		})
	}
}

func TestReplaceVersionInContent(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		oldVersion string
		newVersion string
		want       string
	}{
		{
			name:       "replace quoted version",
			content:    "id \"app\"\nversion \"1.0.0\"\n",
			oldVersion: "1.0.0",
			newVersion: "1.0.1",
			want:       "id \"app\"\nversion \"1.0.1\"\n",
		},
		{
			name:       "replace single quoted version",
			content:    "id 'app'\nversion '1.0.0'\n",
			oldVersion: "1.0.0",
			newVersion: "2.0.0-dev.1",
			want:       "id 'app'\nversion '2.0.0-dev.1'\n",
		},
		{
			name:       "replace unquoted version",
			content:    "id app\nversion 1.0.0\n",
			oldVersion: "1.0.0",
			newVersion: "1.0.1",
			want:       "id app\nversion 1.0.1\n",
		},
		{
			name:       "version at end without newline",
			content:    "id app\nversion 1.0.0",
			oldVersion: "1.0.0",
			newVersion: "1.0.1",
			want:       "id app\nversion 1.0.1",
		},
		{
			name:       "windows line endings",
			content:    "id app\r\nversion 1.0.0\r\n",
			oldVersion: "1.0.0",
			newVersion: "1.0.1",
			want:       "id app\r\nversion 1.0.1\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceVersionInContent(tt.content, tt.oldVersion, tt.newVersion)
			if result != tt.want {
				t.Errorf("replaceVersionInContent() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestVersionManager_ParseAppSCL(t *testing.T) {
	tests := []struct {
		name         string
		parserBlocks []SCLBlock
		parserErr    error
		wantID       string
		wantVersion  string
		wantErr      bool
		errContains  string
	}{
		{
			name: "valid app.scl",
			parserBlocks: []SCLBlock{
				{Key: "id", Name: []string{"com.example.app"}},
				{Key: "version", Name: []string{"1.0.0"}},
			},
			wantID:      "com.example.app",
			wantVersion: "1.0.0",
			wantErr:     false,
		},
		{
			name: "valid app.scl with prerelease",
			parserBlocks: []SCLBlock{
				{Key: "id", Name: []string{"com.test.myapp"}},
				{Key: "version", Name: []string{"1.2.3-dev.5"}},
			},
			wantID:      "com.test.myapp",
			wantVersion: "1.2.3-dev.5",
			wantErr:     false,
		},
		{
			name: "missing id",
			parserBlocks: []SCLBlock{
				{Key: "version", Name: []string{"1.0.0"}},
			},
			wantErr:     true,
			errContains: "id not found",
		},
		{
			name: "missing version",
			parserBlocks: []SCLBlock{
				{Key: "id", Name: []string{"com.example.app"}},
			},
			wantErr:     true,
			errContains: "version not found",
		},
		{
			name:        "parser error",
			parserErr:   &mockError{msg: "parse error"},
			wantErr:     true,
			errContains: "parse error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockParser := &MockSCLParser{
				Result: tt.parserBlocks,
				Err:    tt.parserErr,
			}

			vm := &VersionManager{
				FS:     &MockFileSystem{Files: make(map[string][]byte)},
				Parser: mockParser,
			}

			app, err := vm.ParseAppSCL("/test/app")

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseAppSCL() expected error, got nil")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("ParseAppSCL() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseAppSCL() unexpected error = %v", err)
				return
			}

			if app.ID != tt.wantID {
				t.Errorf("ParseAppSCL() ID = %q, want %q", app.ID, tt.wantID)
			}
			if app.Version != tt.wantVersion {
				t.Errorf("ParseAppSCL() Version = %q, want %q", app.Version, tt.wantVersion)
			}
		})
	}
}

func TestVersionManager_BumpVersion(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string][]byte
		parserBlocks []SCLBlock
		parserErr    error
		appPath      string
		env          string
		bump         string
		wantVersion  string
		wantErr      bool
		errContains  string
	}{
		{
			name: "successful bump",
			files: map[string][]byte{
				"/apps/myapp/app.scl": []byte("id \"com.example.app\"\nversion \"1.0.0\"\n"),
			},
			parserBlocks: []SCLBlock{
				{Key: "id", Name: []string{"com.example.app"}},
				{Key: "version", Name: []string{"1.0.0"}},
			},
			appPath:     "/apps/myapp",
			env:         "dev",
			bump:        "patch",
			wantVersion: "1.0.1-dev.1",
			wantErr:     false,
		},
		{
			name:  "file not found",
			files: map[string][]byte{},
			parserBlocks: []SCLBlock{
				{Key: "id", Name: []string{"com.example.app"}},
				{Key: "version", Name: []string{"1.0.0"}},
			},
			appPath:     "/apps/missing",
			env:         "dev",
			bump:        "patch",
			wantErr:     true,
			errContains: "read app.scl",
		},
		{
			name: "parser error",
			files: map[string][]byte{
				"/apps/myapp/app.scl": []byte("invalid content"),
			},
			parserErr:   &mockError{msg: "syntax error"},
			appPath:     "/apps/myapp",
			env:         "dev",
			bump:        "patch",
			wantErr:     true,
			errContains: "parse app.scl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := &MockFileSystem{Files: tt.files}
			mockParser := &MockSCLParser{
				Result: tt.parserBlocks,
				Err:    tt.parserErr,
			}

			vm := &VersionManager{
				FS:     mockFS,
				Parser: mockParser,
			}

			result, err := vm.BumpVersion(tt.appPath, tt.env, tt.bump)

			if tt.wantErr {
				if err == nil {
					t.Errorf("BumpVersion() expected error, got nil")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("BumpVersion() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("BumpVersion() unexpected error = %v", err)
				return
			}

			if result != tt.wantVersion {
				t.Errorf("BumpVersion() = %q, want %q", result, tt.wantVersion)
			}

			// Verify file was updated
			if len(mockFS.WriteCalls) == 0 {
				t.Error("BumpVersion() did not write to file")
			}
		})
	}
}

func TestVersionManager_BumpVersion_WriteError(t *testing.T) {
	mockFS := &MockFileSystem{
		Files: map[string][]byte{
			"/apps/myapp/app.scl": []byte("id \"com.example.app\"\nversion \"1.0.0\"\n"),
		},
		WriteErr: os.ErrPermission,
	}
	mockParser := &MockSCLParser{
		Result: []SCLBlock{
			{Key: "id", Name: []string{"com.example.app"}},
			{Key: "version", Name: []string{"1.0.0"}},
		},
	}

	vm := &VersionManager{
		FS:     mockFS,
		Parser: mockParser,
	}

	_, err := vm.BumpVersion("/apps/myapp", "dev", "patch")
	if err == nil {
		t.Error("BumpVersion() expected error on write failure, got nil")
		return
	}
	if !containsString(err.Error(), "write app.scl") {
		t.Errorf("BumpVersion() error = %v, want containing 'write app.scl'", err)
	}
}

func TestNewVersionManager(t *testing.T) {
	vm := NewVersionManager("/path/to/scl-parser")
	if vm == nil {
		t.Fatal("NewVersionManager() returned nil")
	}
	if vm.FS == nil {
		t.Error("NewVersionManager() FS is nil")
	}
	if vm.Parser == nil {
		t.Error("NewVersionManager() Parser is nil")
	}
	if vm.ParserPath != "/path/to/scl-parser" {
		t.Errorf("NewVersionManager() ParserPath = %q, want %q", vm.ParserPath, "/path/to/scl-parser")
	}
}

func TestExtractEnvFromPrerelease(t *testing.T) {
	tests := []struct {
		prerelease string
		want       string
	}{
		{"dev.5", "dev"},
		{"staging.10", "staging"},
		{"qa.1", "qa"},
		{"", ""},
		{"alpha", "alpha"},
	}

	for _, tt := range tests {
		t.Run(tt.prerelease, func(t *testing.T) {
			result := extractEnvFromPrerelease(tt.prerelease)
			if result != tt.want {
				t.Errorf("extractEnvFromPrerelease(%q) = %q, want %q", tt.prerelease, result, tt.want)
			}
		})
	}
}

func TestExtractCounter(t *testing.T) {
	tests := []struct {
		prerelease string
		want       int
	}{
		{"dev.5", 5},
		{"staging.10", 10},
		{"", 0},
		{"dev", 0},
		{"qa.100", 100},
	}

	for _, tt := range tests {
		t.Run(tt.prerelease, func(t *testing.T) {
			result := extractCounter(tt.prerelease)
			if result != tt.want {
				t.Errorf("extractCounter(%q) = %d, want %d", tt.prerelease, result, tt.want)
			}
		})
	}
}

func TestBumpMajorMinorPatch(t *testing.T) {
	tests := []struct {
		name     string
		major    int
		minor    int
		patch    int
		bumpType string
		want     string
		wantErr  bool
	}{
		{"patch bump", 1, 2, 3, "patch", "1.2.4", false},
		{"minor bump", 1, 2, 3, "minor", "1.3.0", false},
		{"major bump", 1, 2, 3, "major", "2.0.0", false},
		{"invalid bump", 1, 2, 3, "invalid", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := bumpMajorMinorPatch(tt.major, tt.minor, tt.patch, tt.bumpType)

			if tt.wantErr {
				if err == nil {
					t.Error("bumpMajorMinorPatch() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("bumpMajorMinorPatch() unexpected error = %v", err)
				return
			}

			if result != tt.want {
				t.Errorf("bumpMajorMinorPatch() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestDefaultSCLParser_Parse(t *testing.T) {
	// Test that non-existent parser returns error
	parser := &DefaultSCLParser{ParserPath: "/nonexistent/scl-parser"}

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.scl")
	_ = os.WriteFile(testFile, []byte("key value"), 0644)

	_, err := parser.Parse(testFile)
	if err == nil {
		t.Error("Parse() expected error for nonexistent parser, got nil")
	}
	if !containsString(err.Error(), "execution failed") {
		t.Errorf("Parse() error = %v, want containing 'execution failed'", err)
	}
}

func TestExtractFromBlocks(t *testing.T) {
	tests := []struct {
		name        string
		blocks      []SCLBlock
		wantID      string
		wantVersion string
	}{
		{
			name: "both id and version",
			blocks: []SCLBlock{
				{Key: "id", Name: []string{"com.test.app"}},
				{Key: "version", Name: []string{"2.0.0"}},
			},
			wantID:      "com.test.app",
			wantVersion: "2.0.0",
		},
		{
			name: "only id",
			blocks: []SCLBlock{
				{Key: "id", Name: []string{"com.test.app"}},
			},
			wantID:      "com.test.app",
			wantVersion: "",
		},
		{
			name:        "empty blocks",
			blocks:      []SCLBlock{},
			wantID:      "",
			wantVersion: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, version := extractFromBlocks(tt.blocks)
			if id != tt.wantID {
				t.Errorf("extractFromBlocks() id = %q, want %q", id, tt.wantID)
			}
			if version != tt.wantVersion {
				t.Errorf("extractFromBlocks() version = %q, want %q", version, tt.wantVersion)
			}
		})
	}
}

// Helper types and functions

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
