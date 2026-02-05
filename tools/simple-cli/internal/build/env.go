package build

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// SCL-parser outputs JSON AST in this format:
// [{"key": "set", "name": [...], "children": [...], "type": "block"}]
type sclBlock struct {
	Type     string     `json:"type"`
	Key      string     `json:"key"`
	Children []sclChild `json:"children"`
}

type sclChild struct {
	Type  string `json:"type"`
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// ParseExecutionEnvironment uses scl-parser CLI to extract execution_environment
func ParseExecutionEnvironment(sclParserPath, actionDir string) (string, error) {
	// SCL file is at apps/<app>/records/10_actions.scl
	// actionDir is apps/<app>/actions/<action>/
	// SCL file is at apps/<app>/records/10_actions.scl
	// actionDir is apps/<app>/actions/<action>/
	appDir := filepath.Dir(filepath.Dir(actionDir))
	sclPath := filepath.Join(appDir, "records", "10_actions.scl")
	if _, err := os.Stat(sclPath); os.IsNotExist(err) {
		return "server", nil // default
	}

	cmd := exec.Command(sclParserPath, sclPath)
	output, err := cmd.Output()
	if err != nil {
		return "server", nil // fallback on parse error
	}

	var blocks []sclBlock
	if err := json.Unmarshal(output, &blocks); err != nil {
		return "server", nil
	}

	// Find execution_environment in first set block
	for _, block := range blocks {
		if block.Key == "set" {
			for _, child := range block.Children {
				if child.Key == "execution_environment" {
					if str, ok := child.Value.(string); ok {
						return str, nil
					}
				}
			}
		}
	}
	return "server", nil
}

// ValidateLanguage ensures only TypeScript is supported
func ValidateLanguage(actionDir string) error {
	if !fileExists(filepath.Join(actionDir, "src", "index.ts")) {
		return fmt.Errorf("unsupported language: only TypeScript actions are supported")
	}
	return nil
}
