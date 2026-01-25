package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const ConfigFileName = "contextualizer.json"

type Config struct {
	OutputDir           string   `json:"outputDir"`
	TopLevelDirs        []string `json:"topLevelDirs"`
	Ignore              []string `json:"ignore"`
	ProcessTopLevelDirs bool     `json:"processTopLevelDirs"`
	OpenOutputDirectory bool     `json:"openOutputDirectory"`
}

// DefaultIgnorePatterns matches the legacy/TS defaults
var DefaultIgnorePatterns = []string{
	// Directories
	"node_modules/",
	"dist/",
	"build/",
	"coverage/",
	".git/",
	".vscode/",
	".idea/",
	"__pycache__/",
	".turbo/",
	".turbo/",

	// Files
	"package-lock.json",
	"yarn.lock",
	"bun.lockb",
	"pnpm-lock.yaml",
	".DS_Store",

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
}

var DefaultConfig = Config{
	OutputDir:           ".context",
	TopLevelDirs:        []string{"src"},
	Ignore:              DefaultIgnorePatterns,
	ProcessTopLevelDirs: false,
	OpenOutputDirectory: true,
}

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

	// Let's copy that behavior: Start with defaults, then unmarshal over it.)
	cfg := DefaultConfig

	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Ensure output dir is ignored
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

func GenerateDefault() ([]byte, error) {
	return json.MarshalIndent(DefaultConfig, "", "  ")
}
