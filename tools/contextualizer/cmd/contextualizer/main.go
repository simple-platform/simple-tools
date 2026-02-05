// Package main is the entry point for the Contextualizer tool.
// It orchestrates the configuration loading, file processing, and interactive UI for generating
// context files from a codebase, optimized for LLM usage.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"contextualizer/internal/config"
	"contextualizer/internal/processor"
	"contextualizer/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

var Version = "dev"

func main() {
	initFlag := flag.Bool("init", false, "Initialize default contextualizer.json config file")
	versionFlag := flag.Bool("version", false, "Print current version")
	flag.Parse()

	if *versionFlag {
		fmt.Println(Version)
		os.Exit(0)
	}

	// Handle initialization of configuration
	if *initFlag {
		if err := initializeConfig(); err != nil {
			fmt.Printf("Error initializing: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println("Run 'contextualizer --init' to generate a configuration file.")
		os.Exit(1)
	}

	wd, _ := os.Getwd()
	proc := processor.New(cfg, wd)

	// Scan top level directories to populate the initial UI list
	var subDirs []string
	for _, topDir := range cfg.TopLevelDirs {
		entries, err := os.ReadDir(topDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			// Filter out hidden directories and non-directories
			if e.IsDir() && e.Name()[0] != '.' {
				subDirs = append(subDirs, filepath.Join(wd, topDir, e.Name()))
			}
		}
	}

	// Start the Bubble Tea UI program
	p := tea.NewProgram(ui.NewModel(cfg, proc, wd, subDirs))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

// initializeConfig sets up the default configuration file and updates .gitignore.
// It ensures that the output directory (default: .context) is ignored by git.
func initializeConfig() error {
	data, err := config.GenerateDefault()
	if err != nil {
		return fmt.Errorf("generating default config: %w", err)
	}
	file := config.ConfigFileName
	if err := os.WriteFile(file, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", file, err)
	}
	fmt.Printf("Initialized %s\n", file)

	// Update .gitignore to exclude the output directory
	gitIgnore := ".gitignore"
	if _, err := os.Stat(gitIgnore); err == nil {
		content, err := os.ReadFile(gitIgnore)
		if err != nil {
			fmt.Printf("Warning: Failed to read .gitignore: %v\n", err)
		} else {
			ignorePattern := config.DefaultConfig.OutputDir + "/"
			if !containsLine(string(content), ignorePattern) {
				f, err := os.OpenFile(gitIgnore, os.O_APPEND|os.O_WRONLY, 0644)
				if err != nil {
					fmt.Printf("Warning: Failed to open .gitignore: %v\n", err)
				} else {
					defer func() {
						_ = f.Close()
					}()
					// Ensure we are on a new line
					if len(content) > 0 && content[len(content)-1] != '\n' {
						if _, err := f.WriteString("\n"); err != nil {
							fmt.Printf("Warning: Failed to write newline to .gitignore: %v\n", err)
						}
					}
					if _, err := f.WriteString(ignorePattern + "\n"); err != nil {
						fmt.Printf("Warning: Failed to write to .gitignore: %v\n", err)
					} else {
						fmt.Printf("Added %s to .gitignore\n", ignorePattern)
					}
				}
			}
		}
	}
	return nil
}

// containsLine checks if a specific line exists in the content (trimmed).
func containsLine(content, line string) bool {
	lines := strings.Split(content, "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == strings.TrimSpace(line) {
			return true
		}
	}
	return false
}
