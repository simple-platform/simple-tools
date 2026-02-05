// Package config provides configuration parsing for Simple Platform projects.
// It handles loading various configuration files, such as simple.scl and .env.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// SimpleSCL represents the parsed structure of a simple.scl configuration file.
// It contains critical deployment information such as tenant name and environment definitions.
type SimpleSCL struct {
	Tenant       string                  // Tenant name (e.g., "acme")
	Environments map[string]*Environment // Environment configurations keyed by environment name
}

// Environment represents a specific deployment target configuration.
type Environment struct {
	Name     string // Environment name (e.g., "dev", "staging", "prod")
	Endpoint string // Base endpoint URL (e.g., "acme-dev.on.simple.dev") (can be $ENV_VAR)
	APIKey   string // API key for authentication (usually a $ENV_VAR reference)
}

// DevOpsEndpoint returns the WebSocket URL for the DevOps control plane.
// Format: wss://devops.<endpoint>/socket/websocket
func (e *Environment) DevOpsEndpoint() string {
	return fmt.Sprintf("devops.%s", e.Endpoint)
}

// IdentityEndpoint returns the HTTP URL for the Identity/Auth service.
// Format: identity.<endpoint>
func (e *Environment) IdentityEndpoint() string {
	return fmt.Sprintf("identity.%s", e.Endpoint)
}

// SCLParser abstracts the underlying SCL parsing logic.
// This interface allows for mocking the external 'scl-parser' CLI tool during tests.
type SCLParser interface {
	Parse(path string) ([]SCLBlock, error)
}

// DefaultSCLParser is the production implementation of SCLParser.
// It shells out to the 'scl-parser' binary to parse SCL files into JSON.
type DefaultSCLParser struct {
	ParserPath string
}

// SCLBlock represents a node in the SCL Abstract Syntax Tree (AST).
// The scl-parser tool outputs a flat list of these blocks.
// Types:
//   - "kv" (key-value): A simple property (Key=Value).
//   - "block": A nested structure (Key { ... Children ... }).
type SCLBlock struct {
	Type     string     `json:"type"`     // "kv" or "block"
	Key      string     `json:"key"`      // Identifier (e.g., "tenant", "env")
	Name     string     `json:"name"`     // Optional name for blocks (e.g., "dev" in "env dev { ... }")
	Value    any        `json:"value"`    // Value for KV types
	Children []SCLBlock `json:"children"` // Child nodes for block types
}

// Parse executes the scl-parser CLI tool against the given file path.
// It returns the AST as a slice of SCLBlocks.
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

// Loader is responsible for discovering, reading, and parsing config files.
type Loader struct {
	Parser     SCLParser
	FileReader func(path string) ([]byte, error)
}

// NewLoader creates a new Loader instance.
// parserPath is the absolute path to the scl-parser binary.
func NewLoader(parserPath string) *Loader {
	return &Loader{
		Parser:     &DefaultSCLParser{ParserPath: parserPath},
		FileReader: os.ReadFile,
	}
}

// LoadSimpleSCL loads and parses 'simple.scl' from the specified directory.
// It also attempts to load a '.env' file if present to populate environment variables.
func (l *Loader) LoadSimpleSCL(dir string) (*SimpleSCL, error) {
	path := filepath.Join(dir, "simple.scl")

	// Check if file exists
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("simple.scl not found in %s", dir)
		}
		return nil, fmt.Errorf("cannot access simple.scl: %w", err)
	}

	// Try to load .env file if it exists, but don't fail if it's missing or invalid
	envPath := filepath.Join(dir, ".env")
	if _, err := os.Stat(envPath); err == nil {
		if err := godotenv.Load(envPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to load .env file %s, continuing without it: %v\n", envPath, err)
		}
	}

	// Parse SCL file to JSON AST
	blocks, err := l.Parser.Parse(path)
	if err != nil {
		return nil, err
	}

	return extractConfig(blocks)
}

// GetEnv retrieves the configuration for a named environment.
// It resolves any environment variables references (starting with $) in values.
func (s *SimpleSCL) GetEnv(name string) (*Environment, error) {
	env, ok := s.Environments[name]
	if !ok {
		return nil, fmt.Errorf("environment '%s' not defined in simple.scl", name)
	}

	// Create a copy to avoid modifying the original during resolution
	resolved := &Environment{
		Name:     env.Name,
		Endpoint: resolveEnvVar(env.Endpoint),
		APIKey:   resolveEnvVar(env.APIKey),
	}

	// Validate API key availability after resolution
	if resolved.APIKey == "" {
		if strings.HasPrefix(env.APIKey, "$") {
			return nil, fmt.Errorf("environment variable %s not set", strings.TrimPrefix(env.APIKey, "$"))
		}
		return nil, fmt.Errorf("API key not configured for environment '%s'", name)
	}

	return resolved, nil
}

// resolveEnvVar checks if a value starts with '$' and substitutes it with the OS environment variable.
// Otherwise, it returns the value as is.
func resolveEnvVar(value string) string {
	if strings.HasPrefix(value, "$") {
		envVar := strings.TrimPrefix(value, "$")
		return os.Getenv(envVar)
	}
	return value
}

// extractConfig transforms the SCL AST into a strongly-typed SimpleSCL struct.
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
			// env is a block with Name (the env name) and Children (properties)
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
