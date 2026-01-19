package config

import (
	"os"
	"path/filepath"
	"testing"
)

// MockSCLParser is a mock implementation of SCLParser for testing.
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

func TestLoader_LoadSimpleSCL(t *testing.T) {
	tests := []struct {
		name        string
		setupDir    func(t *testing.T) string
		parser      SCLParser
		wantEnvs    []string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid simple.scl with all environments",
			setupDir: func(t *testing.T) string {
				dir := t.TempDir()
				// Create empty simple.scl (parser is mocked)
				err := os.WriteFile(filepath.Join(dir, "simple.scl"), []byte(""), 0644)
				if err != nil {
					t.Fatal(err)
				}
				return dir
			},
			parser: &MockSCLParser{
				Result: []SCLBlock{
					{Type: "kv", Key: "tenant", Value: "acme"},
					{
						Type: "block",
						Key:  "env",
						Name: "dev",
						Children: []SCLBlock{
							{Type: "kv", Key: "endpoint", Value: "acme-dev.on.simple.dev"},
							{Type: "kv", Key: "api_key", Value: "$SIMPLE_DEV_API_KEY"},
						},
					},
					{
						Type: "block",
						Key:  "env",
						Name: "staging",
						Children: []SCLBlock{
							{Type: "kv", Key: "endpoint", Value: "acme-staging.on.simple.dev"},
							{Type: "kv", Key: "api_key", Value: "$SIMPLE_STAGING_API_KEY"},
						},
					},
					{
						Type: "block",
						Key:  "env",
						Name: "prod",
						Children: []SCLBlock{
							{Type: "kv", Key: "endpoint", Value: "acme.on.simple.dev"},
							{Type: "kv", Key: "api_key", Value: "$SIMPLE_PROD_API_KEY"},
						},
					},
				},
			},
			wantEnvs: []string{"dev", "staging", "prod"},
			wantErr:  false,
		},
		{
			name: "missing simple.scl file",
			setupDir: func(t *testing.T) string {
				return t.TempDir() // Empty directory
			},
			parser:      &MockSCLParser{},
			wantErr:     true,
			errContains: "simple.scl not found",
		},
		{
			name: "parser error",
			setupDir: func(t *testing.T) string {
				dir := t.TempDir()
				err := os.WriteFile(filepath.Join(dir, "simple.scl"), []byte("invalid"), 0644)
				if err != nil {
					t.Fatal(err)
				}
				return dir
			},
			parser: &MockSCLParser{
				Err: &mockError{msg: "syntax error at line 1"},
			},
			wantErr:     true,
			errContains: "syntax error",
		},
		{
			name: "environment missing endpoint",
			setupDir: func(t *testing.T) string {
				dir := t.TempDir()
				err := os.WriteFile(filepath.Join(dir, "simple.scl"), []byte(""), 0644)
				if err != nil {
					t.Fatal(err)
				}
				return dir
			},
			parser: &MockSCLParser{
				Result: []SCLBlock{
					{Type: "kv", Key: "tenant", Value: "test"},
					{
						Type: "block",
						Key:  "env",
						Name: "dev",
						Children: []SCLBlock{
							{Type: "kv", Key: "api_key", Value: "$SIMPLE_DEV_API_KEY"},
							// Missing endpoint
						},
					},
				},
			},
			wantErr:     true,
			errContains: "missing endpoint",
		},
		{
			name: "environment missing api_key",
			setupDir: func(t *testing.T) string {
				dir := t.TempDir()
				err := os.WriteFile(filepath.Join(dir, "simple.scl"), []byte(""), 0644)
				if err != nil {
					t.Fatal(err)
				}
				return dir
			},
			parser: &MockSCLParser{
				Result: []SCLBlock{
					{Type: "kv", Key: "tenant", Value: "test"},
					{
						Type: "block",
						Key:  "env",
						Name: "dev",
						Children: []SCLBlock{
							{Type: "kv", Key: "endpoint", Value: "test-dev.on.simple.dev"},
							// Missing api_key
						},
					},
				},
			},
			wantErr:     true,
			errContains: "missing api_key",
		},
		{
			name: "no environments defined",
			setupDir: func(t *testing.T) string {
				dir := t.TempDir()
				err := os.WriteFile(filepath.Join(dir, "simple.scl"), []byte(""), 0644)
				if err != nil {
					t.Fatal(err)
				}
				return dir
			},
			parser: &MockSCLParser{
				Result: []SCLBlock{
					{Type: "kv", Key: "other_block", Value: "something"},
				},
			},
			wantErr:     true,
			errContains: "tenant not defined",
		},
		{
			name: "env block without name",
			setupDir: func(t *testing.T) string {
				dir := t.TempDir()
				err := os.WriteFile(filepath.Join(dir, "simple.scl"), []byte(""), 0644)
				if err != nil {
					t.Fatal(err)
				}
				return dir
			},
			parser: &MockSCLParser{
				Result: []SCLBlock{
					{Type: "kv", Key: "tenant", Value: "test"},
					{
						Type: "block",
						Key:  "env",
						Name: "", // Empty name
						Children: []SCLBlock{
							{Type: "kv", Key: "endpoint", Value: "test-dev.on.simple.dev"},
							{Type: "kv", Key: "api_key", Value: "$SIMPLE_DEV_API_KEY"},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "no environments defined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setupDir(t)
			loader := &Loader{
				Parser:     tt.parser,
				FileReader: os.ReadFile,
			}

			cfg, err := loader.LoadSimpleSCL(dir)

			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadSimpleSCL() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("LoadSimpleSCL() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("LoadSimpleSCL() unexpected error = %v", err)
				return
			}

			if cfg == nil {
				t.Error("LoadSimpleSCL() returned nil config")
				return
			}

			for _, envName := range tt.wantEnvs {
				if _, ok := cfg.Environments[envName]; !ok {
					t.Errorf("LoadSimpleSCL() missing environment %q", envName)
				}
			}
		})
	}
}

func TestSimpleSCL_GetEnv(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *SimpleSCL
		envName      string
		setupEnvVars map[string]string
		wantEndpoint string
		wantAPIKey   string
		wantErr      bool
		errContains  string
	}{
		{
			name: "environment exists with env var resolved",
			cfg: &SimpleSCL{
				Environments: map[string]*Environment{
					"dev": {
						Name:     "dev",
						Endpoint: "devops.acme.simple.lcl",
						APIKey:   "$TEST_API_KEY",
					},
				},
			},
			envName:      "dev",
			setupEnvVars: map[string]string{"TEST_API_KEY": "secret-key-123"},
			wantEndpoint: "devops.acme.simple.lcl",
			wantAPIKey:   "secret-key-123",
			wantErr:      false,
		},
		{
			name: "environment exists with literal api key",
			cfg: &SimpleSCL{
				Environments: map[string]*Environment{
					"dev": {
						Name:     "dev",
						Endpoint: "devops.acme.simple.lcl",
						APIKey:   "literal-api-key",
					},
				},
			},
			envName:      "dev",
			setupEnvVars: nil,
			wantEndpoint: "devops.acme.simple.lcl",
			wantAPIKey:   "literal-api-key",
			wantErr:      false,
		},
		{
			name: "environment not found",
			cfg: &SimpleSCL{
				Environments: map[string]*Environment{
					"dev": {Name: "dev", Endpoint: "x", APIKey: "y"},
				},
			},
			envName:     "prod",
			wantErr:     true,
			errContains: "not defined",
		},
		{
			name: "env var not set",
			cfg: &SimpleSCL{
				Environments: map[string]*Environment{
					"dev": {
						Name:     "dev",
						Endpoint: "devops.acme.simple.lcl",
						APIKey:   "$UNSET_VAR",
					},
				},
			},
			envName:      "dev",
			setupEnvVars: nil, // Don't set the var
			wantErr:      true,
			errContains:  "UNSET_VAR not set",
		},
		{
			name: "endpoint with env var",
			cfg: &SimpleSCL{
				Environments: map[string]*Environment{
					"dev": {
						Name:     "dev",
						Endpoint: "$DEV_ENDPOINT",
						APIKey:   "key",
					},
				},
			},
			envName:      "dev",
			setupEnvVars: map[string]string{"DEV_ENDPOINT": "custom.endpoint.dev"},
			wantEndpoint: "custom.endpoint.dev",
			wantAPIKey:   "key",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup env vars
			for k, v := range tt.setupEnvVars {
				t.Setenv(k, v)
			}

			env, err := tt.cfg.GetEnv(tt.envName)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetEnv() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("GetEnv() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("GetEnv() unexpected error = %v", err)
				return
			}

			if env.Endpoint != tt.wantEndpoint {
				t.Errorf("GetEnv() endpoint = %q, want %q", env.Endpoint, tt.wantEndpoint)
			}
			if env.APIKey != tt.wantAPIKey {
				t.Errorf("GetEnv() apiKey = %q, want %q", env.APIKey, tt.wantAPIKey)
			}
		})
	}
}

func TestResolveEnvVar(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "env var resolved",
			value:    "$MY_VAR",
			envVars:  map[string]string{"MY_VAR": "resolved-value"},
			expected: "resolved-value",
		},
		{
			name:     "literal value unchanged",
			value:    "literal",
			envVars:  nil,
			expected: "literal",
		},
		{
			name:     "unset env var returns empty",
			value:    "$UNSET",
			envVars:  nil,
			expected: "",
		},
		{
			name:     "dollar sign in middle preserved",
			value:    "prefix$suffix",
			envVars:  nil,
			expected: "prefix$suffix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			result := resolveEnvVar(tt.value)
			if result != tt.expected {
				t.Errorf("resolveEnvVar(%q) = %q, want %q", tt.value, result, tt.expected)
			}
		})
	}
}

func TestExtractEnvironments(t *testing.T) {
	tests := []struct {
		name        string
		blocks      []SCLBlock
		wantEnvs    int
		wantTenant  string
		wantErr     bool
		errContains string
	}{
		{
			name: "multiple environments with tenant",
			blocks: []SCLBlock{
				{Type: "kv", Key: "tenant", Value: "acme"},
				{
					Type: "block",
					Key:  "env",
					Name: "dev",
					Children: []SCLBlock{
						{Type: "kv", Key: "endpoint", Value: "acme-dev.on.simple.dev"},
						{Type: "kv", Key: "api_key", Value: "key1"},
					},
				},
				{
					Type: "block",
					Key:  "env",
					Name: "prod",
					Children: []SCLBlock{
						{Type: "kv", Key: "endpoint", Value: "acme.on.simple.dev"},
						{Type: "kv", Key: "api_key", Value: "key2"},
					},
				},
			},
			wantEnvs:   2,
			wantTenant: "acme",
			wantErr:    false,
		},
		{
			name: "ignores non-env blocks",
			blocks: []SCLBlock{
				{Type: "kv", Key: "tenant", Value: "test"},
				{Type: "kv", Key: "other", Value: "something"},
				{
					Type: "block",
					Key:  "env",
					Name: "dev",
					Children: []SCLBlock{
						{Type: "kv", Key: "endpoint", Value: "dev.example.com"},
						{Type: "kv", Key: "api_key", Value: "key1"},
					},
				},
			},
			wantEnvs:   1,
			wantTenant: "test",
			wantErr:    false,
		},
		{
			name:        "empty blocks",
			blocks:      []SCLBlock{},
			wantErr:     true,
			errContains: "tenant not defined",
		},
		{
			name: "handles non-string values gracefully",
			blocks: []SCLBlock{
				{Type: "kv", Key: "tenant", Value: "myco"},
				{
					Type: "block",
					Key:  "env",
					Name: "dev",
					Children: []SCLBlock{
						{Type: "kv", Key: "endpoint", Value: "dev.example.com"},
						{Type: "kv", Key: "api_key", Value: "key1"},
						{Type: "kv", Key: "extra", Value: 123}, // Non-string value
					},
				},
			},
			wantEnvs:   1,
			wantTenant: "myco",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := extractConfig(tt.blocks)

			if tt.wantErr {
				if err == nil {
					t.Errorf("extractConfig() expected error, got nil")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("extractConfig() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("extractConfig() unexpected error = %v", err)
				return
			}

			if len(cfg.Environments) != tt.wantEnvs {
				t.Errorf("extractConfig() got %d envs, want %d", len(cfg.Environments), tt.wantEnvs)
			}

			if cfg.Tenant != tt.wantTenant {
				t.Errorf("extractConfig() tenant = %q, want %q", cfg.Tenant, tt.wantTenant)
			}
		})
	}
}

func TestNewLoader(t *testing.T) {
	loader := NewLoader("/path/to/scl-parser")

	if loader == nil {
		t.Fatal("NewLoader() returned nil")
	}

	if loader.Parser == nil {
		t.Error("NewLoader() parser is nil")
	}

	if loader.FileReader == nil {
		t.Error("NewLoader() fileReader is nil")
	}

	// Check that parser has correct path
	if parser, ok := loader.Parser.(*DefaultSCLParser); ok {
		if parser.ParserPath != "/path/to/scl-parser" {
			t.Errorf("NewLoader() parser path = %q, want %q", parser.ParserPath, "/path/to/scl-parser")
		}
	} else {
		t.Error("NewLoader() parser is not DefaultSCLParser")
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
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestDefaultSCLParser_Parse(t *testing.T) {
	tests := []struct {
		name        string
		parserPath  string
		setupFile   func(t *testing.T) string
		wantErr     bool
		errContains string
	}{
		{
			name:       "parser not found",
			parserPath: "/nonexistent/scl-parser",
			setupFile: func(t *testing.T) string {
				dir := t.TempDir()
				path := filepath.Join(dir, "test.scl")
				_ = os.WriteFile(path, []byte("key value"), 0644)
				return path
			},
			wantErr:     true,
			errContains: "execution failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setupFile(t)
			parser := &DefaultSCLParser{ParserPath: tt.parserPath}

			_, err := parser.Parse(path)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Parse() expected error, got nil")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("Parse() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Parse() unexpected error = %v", err)
			}
		})
	}
}

func TestSimpleSCL_GetEnv_EmptyAPIKey(t *testing.T) {
	// Test case where API key is empty string (not env var)
	cfg := &SimpleSCL{
		Environments: map[string]*Environment{
			"dev": {
				Name:     "dev",
				Endpoint: "devops.acme.simple.lcl",
				APIKey:   "", // Empty, not prefixed with $
			},
		},
	}

	_, err := cfg.GetEnv("dev")
	if err == nil {
		t.Error("GetEnv() expected error for empty API key, got nil")
		return
	}
	if !containsString(err.Error(), "not configured") {
		t.Errorf("GetEnv() error = %v, want containing 'not configured'", err)
	}
}

func TestLoader_LoadSimpleSCL_StatError(t *testing.T) {
	// Test when we can't stat the file (permission error simulation is tricky,
	// but we can test the path exists check)
	loader := &Loader{
		Parser:     &MockSCLParser{},
		FileReader: os.ReadFile,
	}

	_, err := loader.LoadSimpleSCL("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("LoadSimpleSCL() expected error for nonexistent path")
		return
	}
	if !containsString(err.Error(), "not found") {
		t.Errorf("LoadSimpleSCL() error = %v, want containing 'not found'", err)
	}
}

func TestEnvironment_DevOpsEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{
			name:     "dev environment",
			endpoint: "acme-dev.on.simple.dev",
			want:     "devops.acme-dev.on.simple.dev",
		},
		{
			name:     "prod environment",
			endpoint: "acme.on.simple.dev",
			want:     "devops.acme.on.simple.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &Environment{Endpoint: tt.endpoint}
			got := env.DevOpsEndpoint()
			if got != tt.want {
				t.Errorf("DevOpsEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnvironment_IdentityEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{
			name:     "dev environment",
			endpoint: "acme-dev.on.simple.dev",
			want:     "identity.acme-dev.on.simple.dev",
		},
		{
			name:     "prod environment",
			endpoint: "acme.on.simple.dev",
			want:     "identity.acme.on.simple.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &Environment{Endpoint: tt.endpoint}
			got := env.IdentityEndpoint()
			if got != tt.want {
				t.Errorf("IdentityEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}
