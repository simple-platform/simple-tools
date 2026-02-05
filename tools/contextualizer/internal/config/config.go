package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigFileName is the default name of the configuration file.
const ConfigFileName = "contextualizer.json"

// Config represents the runtime configuration for the Contextualizer.
// It matches the JSON structure of contextualizer.json.
type Config struct {
	// OutputDir is the directory where context files will be saved (e.g., ".context").
	OutputDir string `json:"outputDir"`
	// TopLevelDirs defines which root directories to scan for sub-projects (e.g., "src", "apps").
	TopLevelDirs []string `json:"topLevelDirs"`
	// Ignore is a list of glob patterns to exclude from processing.
	Ignore []string `json:"ignore"`
	// ProcessTopLevelDirs determines if files directly in TopLevelDirs should be included.
	ProcessTopLevelDirs bool `json:"processTopLevelDirs"`
	// OpenOutputDirectory controls whether to open the output dir in the OS file explorer after completion.
	OpenOutputDirectory bool `json:"openOutputDirectory"`
}

// DefaultIgnorePatterns provides a sensible set of defaults for web/software projects.
// It includes common build artifacts, dependency directories, and binary file types.
var DefaultIgnorePatterns = []string{
	// Directories
	"node_modules/",
	"tmp/",
	"dist/",
	"build/",
	"coverage/",
	".git/",
	".tmp/",
	".vscode/",
	".idea/",
	".turbo/",
	"_build/",
	"__pycache__/",
	"burrito_out/",
	"doc/",
	"deps/",
	"simple",

	// Files
	"package-lock.json",
	"yarn.lock",
	"bun.lockb",
	"pnpm-lock.yaml",
	"CHANGELOG.md",
	".gitignore",
	".DS_Store",
	"LICENSE",

	// Extensions / Globs
	"*.log",
	"*.env",
	"*.svg",
	"*.png",
	"*.jpg",
	"*.jpeg",
	"*.gif",
	"*.zip",
	"*.tar",
	"*.gz",
	"*.rar",
	"*.7z",
	"*.pdf",
	"*.doc",
	"*.docx",
	"*.xls",
	"*.xlsx",
	"*.ppt",
	"*.pptx",
	"*.exe",
	"*.dll",
	"*.so",
	"*.dylib",
	"*.bin",
	"*.out",
	"*.sum",
	"*.lock",
}

// DefaultConfig serves as the baseline configuration for new projects.
var DefaultConfig = Config{
	OutputDir:           ".context",
	TopLevelDirs:        []string{"src"},
	Ignore:              DefaultIgnorePatterns,
	ProcessTopLevelDirs: false,
	OpenOutputDirectory: true,
}

// Load attempts to read and parse the contextualizer.json file from the current directory.
// It returns an error if the file is missing or invalid.
func Load() (*Config, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(wd, ConfigFileName)
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("config file %s not found in current directory", ConfigFileName)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Start with defaults, then overwrite with loaded JSON values.
	cfg := DefaultConfig

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Automatically ensure the output directory itself is ignored to prevent recursion.
	outputDirPattern := cfg.OutputDir + "/"
	alreadyIgnored := false
	for _, p := range cfg.Ignore {
		if p == outputDirPattern {
			alreadyIgnored = true
			break
		}
	}
	if !alreadyIgnored {
		cfg.Ignore = append(cfg.Ignore, outputDirPattern)
	}

	return &cfg, nil
}

// GenerateDefault creates a default configuration structure and marshals it to JSON.
func GenerateDefault() ([]byte, error) {
	return json.MarshalIndent(DefaultConfig, "", "  ")
}
