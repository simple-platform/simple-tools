package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"

	"github.com/spf13/cobra"
)

// testCmd represents the command to run tests.
// It integrates with 'vitest' to execute unit and integration tests.
var testCmd = &cobra.Command{
	Use:   "test [app-id]",
	Short: "Run tests for applications",
	Long: `Run Vitest tests for applications, actions, spaces, or record behaviors.

Examples:
  simple test                        # Run all tests
  simple test com.mycompany.crm      # Run tests for a specific app
  simple test com.mycompany.crm -a send-email    # Run tests for specific action
  simple test com.mycompany.crm -b order         # Run tests for specific behavior
  simple test com.mycompany.crm -s analytics     # Run tests for specific space
`,
	// Limit to at most 1 argument (the app-id)
	Args: cobra.MaximumNArgs(1),
	RunE: runTest,
}

func init() {
	testCmd.Flags().StringP("action", "a", "", "Run tests for a specific action")
	testCmd.Flags().StringP("behavior", "b", "", "Run tests for a specific record behavior")
	testCmd.Flags().StringP("space", "s", "", "Run tests for a specific space")
	testCmd.Flags().Bool("coverage", false, "Enable test coverage reporting")
	testCmd.Flags().Bool("json", false, "Output results in JSON format")

	RootCmd.AddCommand(testCmd)
}

// runTest executes the test logic.
// It resolves the target path (app, action, or behavior) and delegates to the vitest binary.
func runTest(cmd *cobra.Command, args []string) error {
	actionName, _ := cmd.Flags().GetString("action")
	behaviorName, _ := cmd.Flags().GetString("behavior")
	spaceName, _ := cmd.Flags().GetString("space")
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

		// Narrow down to specific action, behavior, or space if flags are set
		if actionName != "" {
			targetPath = filepath.Join(targetPath, "actions", actionName)
			if !scaffold.PathExists(fsys, targetPath) {
				return fmt.Errorf("action not found: %s in app %s", actionName, appID)
			}
		} else if behaviorName != "" {
			// Behaviors are scripts specifically in scripts/record-behaviors
			targetPath = filepath.Join(targetPath, "scripts", "record-behaviors")
			if !scaffold.PathExists(fsys, targetPath) {
				return fmt.Errorf("behavior tests not found in app %s", appID)
			}
			// Validate the specific behavior test file exists
			testFile := filepath.Join(targetPath, behaviorName+".test.js")
			if !scaffold.PathExists(fsys, testFile) {
				return fmt.Errorf("behavior test not found: %s in app %s", behaviorName, appID)
			}
		} else if spaceName != "" {
			targetPath = filepath.Join(targetPath, "spaces", spaceName)
			if !scaffold.PathExists(fsys, targetPath) {
				return fmt.Errorf("space not found: %s in app %s", spaceName, appID)
			}
		}
	} else {
		// Default to running tests for all apps
		targetPath = "apps"
	}

	// Resolve absolute target path to ensure we can verify it exists
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Phase 1: Discover all testable directories within the target path
	var testDirs []string

	// If the target is exactly an action, space, or scripts dir, test it directly
	if filepath.Base(filepath.Dir(absTarget)) == "actions" || filepath.Base(filepath.Dir(absTarget)) == "spaces" || filepath.Base(absTarget) == "record-behaviors" {
		testDirs = append(testDirs, targetPath) // Store the relative path
	} else {
		// Traverse targetPath to find all action, space, and behavior script directories
		// e.g. target is "apps" or "apps/com.example.app"
		appsToScan := []string{targetPath}

		// If target is "apps", collect all individual apps
		if filepath.Base(targetPath) == "apps" {
			appsToScan = []string{}
			entries, err := fsys.ReadDir(targetPath)
			if err == nil {
				for _, entry := range entries {
					if entry.IsDir() {
						appsToScan = append(appsToScan, filepath.Join(targetPath, entry.Name()))
					}
				}
			}
		}

		// Gather testable subdirectories for each app
		for _, appDir := range appsToScan {
			// Actions
			actionsDir := filepath.Join(appDir, "actions")
			if entries, err := fsys.ReadDir(actionsDir); err == nil {
				for _, entry := range entries {
					if entry.IsDir() {
						testDirs = append(testDirs, filepath.Join(actionsDir, entry.Name()))
					}
				}
			}
			// Spaces
			spacesDir := filepath.Join(appDir, "spaces")
			if entries, err := fsys.ReadDir(spacesDir); err == nil {
				for _, entry := range entries {
					if entry.IsDir() {
						testDirs = append(testDirs, filepath.Join(spacesDir, entry.Name()))
					}
				}
			}
			// Record Behaviors (one test suite per app realistically)
			behaviorsDir := filepath.Join(appDir, "scripts", "record-behaviors")
			if scaffold.PathExists(fsys, behaviorsDir) {
				// Only add if there are actual test files, otherwise vitest exits 1
				hasTests := false
				if entries, err := fsys.ReadDir(behaviorsDir); err == nil {
					for _, e := range entries {
						if strings.HasSuffix(e.Name(), ".test.js") || strings.HasSuffix(e.Name(), ".test.ts") {
							hasTests = true
							break
						}
					}
				}
				if hasTests {
					testDirs = append(testDirs, behaviorsDir)
				}
			}
		}
	}

	if len(testDirs) == 0 {
		if jsonMode {
			fmt.Println(`{"status":"success","message":"No tests found"}`)
		} else {
			fmt.Println("No tests found to run.")
		}
		return nil
	}

	// Construct Vitest command arguments base
	reporterFlag := "--reporter=verbose"
	if jsonMode {
		reporterFlag = "--reporter=json"
	}

	var passed, failed int
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrency to NumCPU
	limit := runtime.NumCPU()
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)

	for _, tDir := range testDirs {
		wg.Add(1)
		go func(tDir string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var fullArgs []string

			hasPackageJSON := scaffold.PathExists(fsys, filepath.Join(tDir, "package.json"))

			// Use `npm run test` for directories containing a package.json (Actions and Spaces).
			// This ensures package managers (npm/pnpm/yarn) naturally map their own
			// workspace resolution graphs for hoisted dependencies like @simpleplatform/sdk.
			if hasPackageJSON && behaviorName == "" {
				// Only install dependencies if the node_modules directory is completely missing
				hasNodeModules := scaffold.PathExists(fsys, filepath.Join(tDir, "node_modules"))
				if !hasNodeModules {
					if err := build.EnsureDependenciesFunc(tDir); err != nil {
						mu.Lock()
						if !jsonMode {
							fmt.Printf("Error installing dependencies for %s: %v\n", filepath.Base(tDir), err)
						}
						failed++
						mu.Unlock()
						return
					}
				}

				fullArgs = []string{"npm", "run", "test", "--"}
				if jsonMode {
					fullArgs = append(fullArgs, "--reporter=json")
				} else {
					fullArgs = append(fullArgs, "--reporter=verbose")
				}
				if coverage {
					fullArgs = append(fullArgs, "--coverage")
				}
			} else {
				// Fallback for record-behaviors or targets without a package.json test script
				var vitestBin string
				localBin := filepath.Join(tDir, "node_modules", ".bin", "vitest")
				if _, err := os.Stat(localBin); err == nil {
					vitestBin = localBin
				} else {
					cwd, _ := os.Getwd()
					rootBin := filepath.Join(cwd, "node_modules", ".bin", "vitest")
					if _, err := os.Stat(rootBin); err == nil {
						vitestBin = rootBin
					}
				}

				if vitestBin != "" {
					fullArgs = []string{vitestBin, "run", reporterFlag}
				} else {
					fullArgs = []string{"npx", "vitest", "run", reporterFlag}
				}

				if coverage {
					fullArgs = append(fullArgs, "--coverage")
				}

				if behaviorName != "" && filepath.Base(tDir) == "record-behaviors" {
					fullArgs = append(fullArgs, behaviorName+".test.js")
				}
			}

			// Execute FROM the target directory
			execCmd := exec.Command(fullArgs[0], fullArgs[1:]...)
			execCmd.Dir = tDir

			// Vitest strips colors if not directly attached to a TTY.
			// Force colors so the captured combined output retains syntax highlighting.
			execCmd.Env = append(os.Environ(), "FORCE_COLOR=1")

			var stdoutBuf bytes.Buffer
			var stderrBuf bytes.Buffer
			execCmd.Stdout = &stdoutBuf
			execCmd.Stderr = &stderrBuf

			startTime := time.Now()
			err := execCmd.Run()
			duration := time.Since(startTime)

			mu.Lock()
			defer mu.Unlock()

			if !jsonMode {
				fmt.Printf("\n==> Testing %s (took %v)\n", filepath.Base(tDir), duration.Round(time.Millisecond))

				// Always print standard output which contains the pretty Vitest reporting
				if stdoutBuf.Len() > 0 {
					fmt.Print(stdoutBuf.String())
				}

				// Only print stderr if the test actually errored (or if we need to see warnings?)
				// Often Vitest sends warnings to stderr even during successful runs,
				// but let's dump it if things failed to assist debugging.
				if err != nil && stderrBuf.Len() > 0 {
					fmt.Print(stderrBuf.String())
				}
			}

			if err != nil {
				failed++
			} else {
				passed++
			}
		}(tDir)
	}

	wg.Wait()

	if failed > 0 {
		return fmt.Errorf("%d/%d test suites failed", failed, passed+failed)
	}

	if !jsonMode {
		fmt.Printf("\n✅ All %d test suites passed.\n", passed)
	}
	return nil
}
