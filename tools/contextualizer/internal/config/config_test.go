package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestGenerateDefault(t *testing.T) {
	data, err := GenerateDefault()
	if err != nil {
		t.Fatalf("GenerateDefault failed: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Result is not valid JSON: %v", err)
	}

	if cfg.OutputDir != ".context" {
		t.Errorf("Expected DefaultOutput to be .context, got %s", cfg.OutputDir)
	}
	if len(cfg.TopLevelDirs) == 0 {
		t.Error("Expected default TopLevelDirs to be non-empty")
	}
}

func TestLoad_NoFile(t *testing.T) {
	// Should fail if no config file
	tmpDir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Error("Expected Load() to fail when file is missing, but it succeeded")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected Not Found error, got: %v", err)
	}
}

func TestLoad_Success(t *testing.T) {
	tmpDir := t.TempDir()
	cwd, _ := os.Getwd()
	// Write dummy config
	cfg := Config{
		OutputDir: "custom_out",
		TopLevelDirs: []string{"apps"},
		Ignore: []string{"node_modules/"},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(ConfigFileName, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if loaded.OutputDir != "custom_out" {
		t.Errorf("Expected OutputDir='custom_out', got %s", loaded.OutputDir)
	}
	
	// Check strict output dir ignore injection
	hasIgnore := false
	for _, ign := range loaded.Ignore {
		if ign == "custom_out/" {
			hasIgnore = true
		}
	}
	if !hasIgnore {
		t.Error("Expected output dir to be added to ignore list")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.WriteFile(ConfigFileName, []byte("{ invalid json"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Error("Expected error on invalid JSON")
	}
}

func TestLoad_InjectsOutputDir(t *testing.T) {
	tmpDir := t.TempDir()
	cwd, _ := os.Getwd()
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}

	// Config without ignored output dir
	cfg := Config{
		OutputDir: ".context",
		Ignore: []string{}, 
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(ConfigFileName, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	found := false
	for _, p := range loaded.Ignore {
		if p == ".context/" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Load should automatically inject OutputDir into ignore list")
	}
}
