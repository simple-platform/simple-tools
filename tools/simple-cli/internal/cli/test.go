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

// testCmd represents the command to run tests.
// It integrates with 'vitest' to execute unit and integration tests.
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
	// Limit to at most 1 argument (the app-id)
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

// runTest executes the test logic.
// It resolves the target path (app, action, or behavior) and delegates to the vitest binary.
func runTest(cmd *cobra.Command, args []string) error {
	actionName, _ := cmd.Flags().GetString("action")
	behaviorName, _ := cmd.Flags().GetString("behavior")
	coverage, _ := cmd.Flags().GetBool("coverage")
	jsonMode, _ := cmd.Flags().GetBool("json")

	// Verify we are in a valid monorepo root by checking for "apps" directory.
	fsys := fsx.OSFileSystem{}
	if !scaffold.PathExists(fsys, "apps") {
		return fmt.Errorf("apps directory not found. Are you in a Simple Platform monorepo root?")
	}

	var targetPath string
	if len(args) > 0 {
		appID := args[0]
		targetPath = filepath.Join("apps", appID)

		// Validate app exists to provide a friendly error early
		if !scaffold.PathExists(fsys, targetPath) {
			return fmt.Errorf("app not found: %s", appID)
		}

		// Narrow down to specific action or behavior if flags are set
		if actionName != "" {
			targetPath = filepath.Join(targetPath, "actions", actionName)
			if !scaffold.PathExists(fsys, targetPath) {
				return fmt.Errorf("action not found: %s in app %s", actionName, appID)
			}
		} else if behaviorName != "" {
			// Behaviors are scripts specifically in scripts/record-behaviors
			targetPath = filepath.Join(targetPath, "scripts", "record-behaviors", behaviorName+".test.js")
			if !scaffold.PathExists(fsys, targetPath) {
				return fmt.Errorf("behavior test not found: %s in app %s", behaviorName, appID)
			}
		}
	} else {
		// Default to running tests for all apps
		targetPath = "apps"
	}

	// Resolve absolute target path to ensure vitest runs in the correct context
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Construct Vitest command arguments
	reporterFlag := "--reporter=verbose"
	if jsonMode {
		reporterFlag = "--reporter=json"
	}

	// Strategy: Prefer local vitest binary in node_modules/.bin for version consistency.
	// 1. Check target's node_modules (if inside an app)
	// 2. Check root node_modules
	// 3. Fallback to npx (system wide or on-demand install)
	vitestBin := filepath.Join(absTarget, "node_modules", ".bin", "vitest")

	// If not found in target, fallback to root node_modules (common in monorepos)
	if _, err := os.Stat(vitestBin); os.IsNotExist(err) {
		cwd, _ := os.Getwd()
		vitestBin = filepath.Join(cwd, "node_modules", ".bin", "vitest")
	}

	var fullArgs []string
	if _, err := os.Stat(vitestBin); err == nil {
		// Found binary, use absolute path to avoid ambiguity
		fullArgs = []string{vitestBin, "run", reporterFlag}
	} else {
		// Fallback to npx to attempt execution using package.json dependencies
		fullArgs = []string{"npx", "vitest", "run", reporterFlag}
	}

	if coverage {
		fullArgs = append(fullArgs, "--coverage")
	}

	// Log command for transparency, unless in JSON mode where stdout must strictly be JSON
	if !jsonMode {
		fmt.Printf("Running: %v in %s\n", fullArgs, absTarget)
	}

	execCmd := exec.Command(fullArgs[0], fullArgs[1:]...)
	execCmd.Dir = absTarget // Set working directory to the target (or app root)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		// Preserve vitest exit code if possible
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		return fmt.Errorf("failed to run tests: %w", err)
	}

	return nil
}
