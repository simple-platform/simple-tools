// Package config provides configuration parsing for Simple Platform projects.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SimpleSCL represents the parsed simple.scl configuration file.
// It contains tenant name and environment definitions for deployment targets.
type SimpleSCL struct {
	Tenant       string                  // Tenant name (e.g., "acme")
	Environments map[string]*Environment // Environment configurations
}

// Environment represents a deployment target configuration.
type Environment struct {
	Name     string // Environment name (e.g., "dev", "staging", "prod")
	Endpoint string // Base endpoint (e.g., "acme-dev.on.simple.dev")
	APIKey   string // API key or $ENV_VAR reference
}

// DevOpsEndpoint returns the WebSocket URL for DevOps channel.
// Format: wss://devops.<endpoint>/socket/websocket
func (e *Environment) DevOpsEndpoint() string {
	return fmt.Sprintf("devops.%s", e.Endpoint)
}

// IdentityEndpoint returns the HTTP URL for identity/auth.
// Format: identity.<endpoint>
func (e *Environment) IdentityEndpoint() string {
	return fmt.Sprintf("identity.%s", e.Endpoint)
}

// SCLParser abstracts the scl-parser CLI for testing.
type SCLParser interface {
	Parse(path string) ([]SCLBlock, error)
}

// DefaultSCLParser uses the scl-parser CLI binary.
type DefaultSCLParser struct {
	ParserPath string
}

// SCLBlock represents an element in the SCL AST.
// The scl-parser outputs two types:
//   - "kv" (key-value): has Key and Value fields
//   - "block": has Key, Name (string), and Children
type SCLBlock struct {
	Type     string     `json:"type"`     // "kv" or "block"
	Key      string     `json:"key"`      // e.g., "tenant", "env"
	Name     string     `json:"name"`     // For blocks: the block name (e.g., "dev")
	Value    any        `json:"value"`    // For kv: the value
	Children []SCLBlock `json:"children"` // For blocks: child elements
}

// Parse executes scl-parser CLI and returns the AST.
func (p *DefaultSCLParser) Parse(path string) ([]SCLBlock, error) {
	cmd := exec.Command(p.ParserPath, path)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("scl-parser failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("scl-parser execution failed: %w", err)
	}

	var blocks []SCLBlock
	if err := json.Unmarshal(output, &blocks); err != nil {
		return nil, fmt.Errorf("failed to parse scl-parser output: %w", err)
	}

	return blocks, nil
}

// Loader handles loading and parsing of simple.scl files.
type Loader struct {
	Parser     SCLParser
	FileReader func(path string) ([]byte, error)
}

// NewLoader creates a Loader with default dependencies.
func NewLoader(parserPath string) *Loader {
	return &Loader{
		Parser:     &DefaultSCLParser{ParserPath: parserPath},
		FileReader: os.ReadFile,
	}
}

// LoadSimpleSCL parses simple.scl from the given directory.
func (l *Loader) LoadSimpleSCL(dir string) (*SimpleSCL, error) {
	path := filepath.Join(dir, "simple.scl")

	// Check if file exists
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("simple.scl not found in %s", dir)
		}
		return nil, fmt.Errorf("cannot access simple.scl: %w", err)
	}

	// Parse SCL file to JSON AST
	blocks, err := l.Parser.Parse(path)
	if err != nil {
		return nil, err
	}

	return extractConfig(blocks)
}

// GetEnv returns the environment config with $ENV_VAR values resolved.
func (s *SimpleSCL) GetEnv(name string) (*Environment, error) {
	env, ok := s.Environments[name]
	if !ok {
		return nil, fmt.Errorf("environment '%s' not defined in simple.scl", name)
	}

	// Create a copy to avoid modifying the original
	resolved := &Environment{
		Name:     env.Name,
		Endpoint: resolveEnvVar(env.Endpoint),
		APIKey:   resolveEnvVar(env.APIKey),
	}

	// Validate API key is set
	if resolved.APIKey == "" {
		if strings.HasPrefix(env.APIKey, "$") {
			return nil, fmt.Errorf("environment variable %s not set", strings.TrimPrefix(env.APIKey, "$"))
		}
		return nil, fmt.Errorf("API key not configured for environment '%s'", name)
	}

	return resolved, nil
}

// resolveEnvVar resolves $ENV_VAR references to their values.
func resolveEnvVar(value string) string {
	if strings.HasPrefix(value, "$") {
		envVar := strings.TrimPrefix(value, "$")
		return os.Getenv(envVar)
	}
	return value
}

// extractConfig parses the SCL AST to extract tenant and environment definitions.
func extractConfig(blocks []SCLBlock) (*SimpleSCL, error) {
	cfg := &SimpleSCL{Environments: make(map[string]*Environment)}

	for _, block := range blocks {
		switch block.Key {
		case "tenant":
			// tenant is a KV, so Value contains the tenant name
			if s, ok := block.Value.(string); ok {
				cfg.Tenant = s
			}
		case "env":
			// env is a block with Name and Children
			if block.Name != "" {
				envName := block.Name
				env := &Environment{Name: envName}

				for _, child := range block.Children {
					if child.Type == "kv" {
						switch child.Key {
						case "endpoint":
							if s, ok := child.Value.(string); ok {
								env.Endpoint = s
							}
						case "api_key":
							if s, ok := child.Value.(string); ok {
								env.APIKey = s
							}
						}
					}
				}

				if env.Endpoint == "" {
					return nil, fmt.Errorf("environment '%s' missing endpoint", envName)
				}
				if env.APIKey == "" {
					return nil, fmt.Errorf("environment '%s' missing api_key", envName)
				}

				cfg.Environments[envName] = env
			}
		}
	}

	if cfg.Tenant == "" {
		return nil, fmt.Errorf("tenant not defined in simple.scl")
	}

	if len(cfg.Environments) == 0 {
		return nil, fmt.Errorf("no environments defined in simple.scl")
	}

	return cfg, nil
}
