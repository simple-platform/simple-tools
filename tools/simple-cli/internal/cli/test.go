package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

// testCmd represents the test command
var testCmd = &cobra.Command{
	Use:   "test [app-id]",
	Short: "Run tests for applications",
	Long: `Run Vitest tests for applications, actions, or record behaviors.

Examples:
  simple test                        # Run all tests
  simple test com.mycompany.crm      # Run tests for a specific app
  simple test com.mycompany.crm -a send-email    # Run tests for specific action
  simple test com.mycompany.crm -b order         # Run tests for specific behavior
`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTest,
}

func init() {
	testCmd.Flags().StringP("action", "a", "", "Run tests for a specific action")
	testCmd.Flags().StringP("behavior", "b", "", "Run tests for a specific record behavior")
	testCmd.Flags().Bool("coverage", false, "Enable test coverage reporting")
	testCmd.Flags().Bool("json", false, "Output results in JSON format")

	RootCmd.AddCommand(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	actionName, _ := cmd.Flags().GetString("action")
	behaviorName, _ := cmd.Flags().GetString("behavior")
	coverage, _ := cmd.Flags().GetBool("coverage")
	jsonMode, _ := cmd.Flags().GetBool("json")

	// Verify monorepo
	fsys := fsx.OSFileSystem{}
	if !scaffold.PathExists(fsys, "apps") {
		return fmt.Errorf("apps directory not found. Are you in a Simple Platform monorepo root?")
	}

	var targetPath string
	if len(args) > 0 {
		appID := args[0]
		targetPath = filepath.Join("apps", appID)

		// Validate app exists
		if !scaffold.PathExists(fsys, targetPath) {
			return fmt.Errorf("app not found: %s", appID)
		}

		if actionName != "" {
			targetPath = filepath.Join(targetPath, "actions", actionName)
			if !scaffold.PathExists(fsys, targetPath) {
				return fmt.Errorf("action not found: %s in app %s", actionName, appID)
			}
		} else if behaviorName != "" {
			targetPath = filepath.Join(targetPath, "scripts", "record-behaviors", behaviorName+".test.js")
			if !scaffold.PathExists(fsys, targetPath) {
				return fmt.Errorf("behavior test not found: %s in app %s", behaviorName, appID)
			}
		}
	} else {
		// Run all tests
		targetPath = "apps"
	}

	// Construct Vitest command
	vitestArgs := []string{"vitest", "run", targetPath}

	if coverage {
		vitestArgs = append(vitestArgs, "--coverage")
	}

	if jsonMode {
		vitestArgs = append(vitestArgs, "--reporter=json")
	} else {
		vitestArgs = append(vitestArgs, "--reporter=verbose")
	}

	// Use npx to run vitest from local node_modules
	fullArgs := append([]string{"npx"}, vitestArgs...)

	// Print command for clarity (unless in JSON mode where it might corrupt parsing)
	if !jsonMode {
		fmt.Printf("Running: %s\n", fmt.Sprint(fullArgs))
	}

	execCmd := exec.Command(fullArgs[0], fullArgs[1:]...)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		// Exit with same code as vitest
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		return fmt.Errorf("failed to run tests: %w", err)
	}

	return nil
}
