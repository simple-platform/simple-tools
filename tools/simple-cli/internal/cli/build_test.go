package cli

import (
	"os"
	"simple-cli/internal/fsx"
	"strings"
	"testing"
)

func TestRunBuild(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		buildAll bool
		wantErr  bool
		errCheck func(error) bool
	}{
		{
			name:     "build all success",
			args:     []string{},
			buildAll: true,
			wantErr:  false,
		},
		{
			name:     "build all with args error",
			args:     []string{"target"},
			buildAll: true,
			wantErr:  true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "cannot use --all with a target argument")
			},
		},
		{
			name:     "build target success",
			args:     []string{"myapp/action"},
			buildAll: false,
			wantErr:  false,
		},
		{
			name:     "build no args error",
			args:     []string{},
			buildAll: false,
			wantErr:  true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "requires a target argument")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			buildAll = tt.buildAll // Set global flag (in a real app, passing this via config is better)

			// Create temp dir for test
			tmpDir := t.TempDir()
			oldWd, _ := os.Getwd()
			os.Chdir(tmpDir)
			defer os.Chdir(oldWd)

			// Create dummy targets for success cases
			if !tt.wantErr {
				if tt.name == "build target success" {
					os.MkdirAll("myapp/action", 0755)
				}
				if tt.name == "build all success" {
					// No specific target needed for all, but good to have env clean
				}
			}

			// Capture output
			// (If we wanted to capture stdout, we'd need to redirect os.Stdout or inject a Writer,
			// but for now we just check errors)

			err := runBuild(fsx.OSFileSystem{}, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("runBuild() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errCheck != nil {
				if !tt.errCheck(err) {
					t.Errorf("runBuild() unexpected error = %v", err)
				}
			}
		})
	}
	// Reset global
	buildAll = false
}
