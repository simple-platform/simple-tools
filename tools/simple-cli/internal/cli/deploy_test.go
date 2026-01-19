package cli

import (
	"os"
	"path/filepath"
	"simple-cli/internal/fsx"
	"testing"
)

func TestRunDeploy(t *testing.T) {
	// Save original flag values
	origEnv := deployEnv
	origBump := deployBump
	origDryRun := deployDryRun
	defer func() {
		deployEnv = origEnv
		deployBump = origBump
		deployDryRun = origDryRun
	}()

	tests := []struct {
		name        string
		args        []string
		env         string
		bump        string
		dryRun      bool
		setupDir    func(t *testing.T, dir string)
		wantErr     bool
		errContains string
	}{
		{
			name:        "missing env flag",
			args:        []string{"apps/myapp"},
			env:         "", // Missing --env
			wantErr:     true,
			errContains: "--env flag is required",
		},
		{
			name:        "app not found",
			args:        []string{"apps/nonexistent"},
			env:         "dev",
			wantErr:     true,
			errContains: "not found",
		},
		{
			name: "missing simple.scl",
			args: []string{"apps/myapp"},
			env:  "dev",
			bump: "patch",
			setupDir: func(t *testing.T, dir string) {
				// Create app directory but no simple.scl
				appDir := filepath.Join(dir, "apps", "myapp")
				_ = os.MkdirAll(appDir, 0755)
				_ = os.WriteFile(filepath.Join(appDir, "app.scl"), []byte("id test\nversion 1.0.0"), 0644)
			},
			wantErr:     true,
			errContains: "simple.scl", // Now fails at simple.scl loading since scl-parser is available
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup directory
			tmpDir := t.TempDir()
			oldWd, _ := os.Getwd()
			_ = os.Chdir(tmpDir)
			defer func() { _ = os.Chdir(oldWd) }()

			// Set flags
			deployEnv = tt.env
			deployBump = tt.bump
			deployDryRun = tt.dryRun

			// Run setup if provided
			if tt.setupDir != nil {
				tt.setupDir(t, tmpDir)
			}

			err := runDeploy(fsx.OSFileSystem{}, tt.args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("runDeploy() expected error, got nil")
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("runDeploy() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("runDeploy() unexpected error = %v", err)
			}
		})
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
