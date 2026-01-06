package cli

import (
	"simple-cli/internal/fsx"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
			cmd := &cobra.Command{}
			buildAll = tt.buildAll // Set global flag (in a real app, passing this via config is better)

			// Capture output
			// (If we wanted to capture stdout, we'd need to redirect os.Stdout or inject a Writer,
			// but for now we just check errors)

			err := runBuild(fsx.OSFileSystem{}, cmd, tt.args)
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
