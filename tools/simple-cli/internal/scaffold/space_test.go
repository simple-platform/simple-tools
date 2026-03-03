package scaffold

import (
	"errors"
	"strings"
	"testing"

	"simple-cli/internal/fsx"
)

func TestCreateSpaceStructure_Success(t *testing.T) {
	written := make(map[string][]byte)

	mockFS := &mockWriteTrackingFS{
		written: written,
		statFn: func(name string) bool {
			// Simulate that app exists but space dir and 10_spaces.scl don't
			if strings.Contains(name, "10_spaces.scl") {
				return false
			}
			return strings.Contains(name, "apps/com.acme.app") && !strings.Contains(name, "spaces/my-space")
		},
	}

	mockTpl := &fsx.MockTemplateFS{
		Files: map[string][]byte{
			"templates/space/package.json":       []byte(`{"name": "{{.SpaceName}}"}`),
			"templates/space/vite.config.ts":     []byte(`// vite config`),
			"templates/space/vitest.config.ts":   []byte(`// vitest config`),
			"templates/space/tsconfig.json":      []byte(`// ts config`),
			"templates/space/index.html":         []byte(`<title>{{.DisplayName}}</title>`),
			"templates/space/src/main.tsx":       []byte(`// main`),
			"templates/space/src/App.tsx":        []byte(`<h1>{{.DisplayName}}</h1>`),
			"templates/space/tests/App.test.tsx": []byte(`// test`),
			"templates/space/10_spaces.scl":      []byte("set dev_simple_system.space, {{.SpaceNameScl}} { display_name \"{{.DisplayName}}\" }"),
		},
	}

	cfg := SpaceConfig{
		AppID:       "com.acme.app",
		SpaceName:   "my-space",
		DisplayName: "My Space",
		Description: "A test space",
	}

	err := CreateSpaceStructure(mockFS, mockTpl, "/root", cfg)
	if err != nil {
		t.Fatalf("CreateSpaceStructure failed: %v", err)
	}

	// Verify package.json was written with correct content
	pkgJson := written["/root/apps/com.acme.app/spaces/my-space/package.json"]
	if !strings.Contains(string(pkgJson), `"name": "my-space"`) {
		t.Errorf("package.json missing or incorrect, got: %s", string(pkgJson))
	}

	appTsx := written["/root/apps/com.acme.app/spaces/my-space/src/App.tsx"]
	if !strings.Contains(string(appTsx), `<h1>My Space</h1>`) {
		t.Errorf("App.tsx missing or incorrect, got: %s", string(appTsx))
	}

	// Verify 10_spaces.scl was created
	spacesScl := written["/root/apps/com.acme.app/records/10_spaces.scl"]
	if !strings.Contains(string(spacesScl), "my_space") {
		t.Errorf("10_spaces.scl doesn't contain space name, got: %s", string(spacesScl))
	}
}

func TestCreateSpaceStructure_AppNotExists(t *testing.T) {
	mockFS := &mockWriteTrackingFS{} // Default: everything returns NotExist

	cfg := SpaceConfig{
		AppID:     "nonexistent",
		SpaceName: "test",
	}

	err := CreateSpaceStructure(mockFS, &fsx.MockTemplateFS{}, "/root", cfg)
	if err == nil {
		t.Error("Expected error for non-existent app")
	}
	if !strings.Contains(err.Error(), "app does not exist") {
		t.Errorf("Expected 'app does not exist' error, got: %v", err)
	}
}

func TestCreateSpaceStructure_SpaceExists(t *testing.T) {
	// Mock SCL check
	origCheck := checkSCLEntityMatchType
	defer func() { checkSCLEntityMatchType = origCheck }()
	checkSCLEntityMatchType = func(path, block, typ, name string) (bool, error) {
		return false, nil // Assume it doesn't exist in SCL, but space dir exists
	}

	mockFS := &mockWriteTrackingFS{
		statFn: func(name string) bool {
			// Both app and space dir exist
			return strings.Contains(name, "apps/com.test")
		},
	}

	cfg := SpaceConfig{
		AppID:     "com.test",
		SpaceName: "existing-space",
	}

	err := CreateSpaceStructure(mockFS, &fsx.MockTemplateFS{}, "/root", cfg)
	if err == nil {
		t.Error("Expected error for existing space")
	}
	if !strings.Contains(err.Error(), "space directory already exists") {
		t.Errorf("Expected 'space directory already exists' error, got: %v", err)
	}
}

func TestCreateSpaceStructure_DuplicateSCL(t *testing.T) {
	// Mock SCL check
	origCheck := checkSCLEntityMatchType
	defer func() { checkSCLEntityMatchType = origCheck }()
	checkSCLEntityMatchType = func(path, block, typ, name string) (bool, error) {
		if name == "duplicate_space" {
			return true, nil
		}
		return false, nil
	}

	// Space directory doesn't exist, but it's present in SCL
	mockFS := &mockWriteTrackingFS{
		statFn: func(name string) bool {
			// App exists, SCL exists, space dir doesn't
			if strings.Contains(name, "10_spaces.scl") {
				return true
			}
			return strings.Contains(name, "apps/com.test") && !strings.Contains(name, "spaces/duplicate-space")
		},
		files: map[string][]byte{
			"/root/apps/com.test/records/10_spaces.scl": []byte("set dev_simple_system.space, duplicate_space {"),
		},
	}

	cfg := SpaceConfig{
		AppID:     "com.test",
		SpaceName: "duplicate-space",
	}

	err := CreateSpaceStructure(mockFS, &fsx.MockTemplateFS{}, "/root", cfg)
	if err == nil {
		t.Error("Expected error for duplicate space in SCL")
	}
	if !strings.Contains(err.Error(), "space already exists in records") {
		t.Errorf("Expected 'space already exists in records' error, got: %v", err)
	}
}

func TestCreateSpaceStructure_MkdirError(t *testing.T) {
	mockFS := &mockWriteTrackingFS{
		statFn: func(name string) bool {
			return name == "/root/apps/com.test"
		},
		mkdirErr: errors.New("permission denied"),
	}

	cfg := SpaceConfig{
		AppID:     "com.test",
		SpaceName: "my-space",
	}

	err := CreateSpaceStructure(mockFS, &fsx.MockTemplateFS{}, "/root", cfg)
	if err == nil {
		t.Error("Expected mkdir error")
	}
	if !strings.Contains(err.Error(), "failed to create directory") {
		t.Errorf("Expected 'failed to create directory' error, got: %v", err)
	}
}

func TestAppendSpaceRecord_NewFile(t *testing.T) {
	written := make(map[string][]byte)

	mockFS := &mockWriteTrackingFS{
		written: written,
		statFn:  func(name string) bool { return false }, // File doesn't exist
	}

	mockTpl := &fsx.MockTemplateFS{
		Files: map[string][]byte{
			"templates/space/10_spaces.scl": []byte("set space, {{.SpaceName}}"),
		},
	}

	data := map[string]string{"SpaceName": "test-space"}
	err := appendSpaceRecord(mockFS, mockTpl, "/path/10_spaces.scl", data)
	if err != nil {
		t.Fatalf("appendSpaceRecord failed: %v", err)
	}

	content := written["/path/10_spaces.scl"]
	if !strings.Contains(string(content), "test-space") {
		t.Errorf("Expected space name in output, got: %s", string(content))
	}
}

func TestAppendSpaceRecord_AppendToExisting(t *testing.T) {
	written := make(map[string][]byte)

	mockFS := &mockWriteTrackingFS{
		written: written,
		statFn:  func(name string) bool { return true }, // File exists
		files: map[string][]byte{
			"/path/10_spaces.scl": []byte("# existing content"),
		},
	}

	mockTpl := &fsx.MockTemplateFS{
		Files: map[string][]byte{
			"templates/space/10_spaces.scl": []byte("set space, {{.SpaceName}}"),
		},
	}

	data := map[string]string{"SpaceName": "new-space"}
	err := appendSpaceRecord(mockFS, mockTpl, "/path/10_spaces.scl", data)
	if err != nil {
		t.Fatalf("appendSpaceRecord failed: %v", err)
	}

	content := written["/path/10_spaces.scl"]
	if !strings.Contains(string(content), "# existing content") {
		t.Errorf("Expected existing content preserved, got: %s", string(content))
	}
	if !strings.Contains(string(content), "new-space") {
		t.Errorf("Expected new space added, got: %s", string(content))
	}
}
