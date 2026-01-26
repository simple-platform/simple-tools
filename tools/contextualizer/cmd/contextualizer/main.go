package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"contextualizer/internal/config"
	"contextualizer/internal/processor"
	"contextualizer/internal/ui"
)


var Version = "dev"

func main() {
	initFlag := flag.Bool("init", false, "Initialize contextualizer.json")
	versionFlag := flag.Bool("version", false, "Print version")
	flag.Parse()

	if *versionFlag {
		fmt.Println(Version)
		os.Exit(0)
	}

	if *initFlag {
		data, err := config.GenerateDefault()
		if err != nil {
			fmt.Printf("Error generating default config: %v\n", err)
			os.Exit(1)
		}
		file := config.ConfigFileName
		if err := os.WriteFile(file, data, 0644); err != nil {
			fmt.Printf("Error writing %s: %v\n", file, err)
			os.Exit(1)
		}
		fmt.Printf("Initialized %s\n", file)

		// Update .gitignore if it exists
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
							if err := f.Close(); err != nil {
								fmt.Printf("Warning: Failed to close .gitignore: %v\n", err)
							}
						}()
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

		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println("Run 'contextualizer --init' to generate a configuration file.")
		os.Exit(1)
	}

	wd, _ := os.Getwd()
	proc := processor.New(cfg, wd)
	
	// Scan top level dirs
	var subDirs []string
	for _, topDir := range cfg.TopLevelDirs {
		entries, err := os.ReadDir(topDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && e.Name()[0] != '.' { // Skip hidden
				subDirs = append(subDirs, filepath.Join(wd, topDir, e.Name()))
			}
		}
	}

	p := tea.NewProgram(ui.NewModel(cfg, proc, wd, subDirs))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

func containsLine(content, line string) bool {
	lines := strings.Split(content, "\n")
	for _, l := range lines {
		if strings.TrimSpace(l) == strings.TrimSpace(line) {
			return true
		}
	}
	return false
}
