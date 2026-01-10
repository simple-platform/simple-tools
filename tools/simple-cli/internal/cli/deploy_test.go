package cli

import (
	"os"
	"simple-cli/internal/fsx"
	"testing"
)

func TestRunDeploy(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "deploy success",
			args:    []string{"myapp"},
			wantErr: false,
		},
		// Deploy command args are checked by Cobra (ExactArgs(1)) before calling RunE,
		// but since we call runDeploy directly we should rely on the slice passed to us.
		// However, runDeploy assumes args[0] exists so we should match that expectation
		// or handle panic if args is empty (though Cobra guarantees it won't be if wired correctly).
		// For unit testing the function, we should pass valid args.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			// Setup fs
			tmpDir := t.TempDir()
			oldWd, _ := os.Getwd()
			_ = os.Chdir(tmpDir)
			defer func() { _ = os.Chdir(oldWd) }()

			if !tt.wantErr {
				// Create app dir
				_ = os.MkdirAll("apps/"+tt.args[0], 0755)
			}

			err := runDeploy(fsx.OSFileSystem{}, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("runDeploy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
