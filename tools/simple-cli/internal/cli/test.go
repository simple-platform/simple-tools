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
// It runs each target's own test runner: vitest for TypeScript and JavaScript,
// cargo for Rust actions.
var testCmd = &cobra.Command{
	Use:   "test [app-id]",
	Short: "Run tests for applications",
	Long: `Run tests for applications, actions, spaces, or record behaviors.

TypeScript and JavaScript targets run under Vitest; Rust actions run under
'cargo test', on this machine, with no wasm build and no emulator.

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
// It resolves the target path (app, action, or behavior) and delegates to that
// target's own test runner.
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

	// Phase 2: decide which runner each directory gets.
	//
	// A Rust action's tests are 'cargo test' and they run on the host: the test
	// seam stands in for the platform, so nothing here needs a wasm build or an
	// emulator, which is what makes them fast enough to run on every save.
	//
	// Which language a directory holds is asked of build.DetectActionLanguage
	// rather than answered again here, so that the runner this command picks
	// and the compiler the build path picks can never disagree about the same
	// directory.
	//
	// Its error is not reported here, and is not a second refusal waiting to
	// happen. This list also holds spaces and record-behaviour scripts, which
	// are not actions and hold no action source, so being unable to name a
	// language is the ordinary answer for most of it. Where it does mean
	// something — an action with no source, or with two — 'simple build' is
	// what says so, by name; saying it twice in two different sentences would
	// leave a developer looking for two problems.
	rustDirs := make(map[string]bool, len(testDirs))
	rustFound := false
	for _, tDir := range testDirs {
		if lang, err := build.DetectActionLanguage(tDir); err == nil && lang == build.LanguageRust {
			rustDirs[tDir] = true
			rustFound = true
		}
	}

	// Refuse up front rather than letting each suite fail with "executable file
	// not found": one clear sentence beats one cryptic line per action.
	if rustFound {
		if _, err := exec.LookPath("cargo"); err != nil {
			return fmt.Errorf("cargo not found on PATH, and this run includes Rust actions. Install a Rust toolchain (https://rustup.rs) to run their tests")
		}
	}

	// --coverage has no counterpart in cargo: coverage for Rust is a separate
	// subcommand (cargo-llvm-cov) rather than a flag on the test runner, and
	// installing one on a developer's behalf is not this command's business.
	// Say so once, and run the tests without it, so a mixed app still reports
	// coverage for the targets that can produce it.
	if rustFound && coverage && !jsonMode {
		fmt.Println("Note: --coverage does not apply to Rust actions; their tests run without it.")
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

			// Rust first, and before the package.json question rather than
			// after it: a Rust action carries no package.json, so without this
			// branch it would fall through to the vitest fallback and be handed
			// to a runner that has nothing to run.
			//
			// cargo resolves and fetches the crate's dependencies itself, so
			// there is no install step to run beforehand the way there is for
			// npm.
			if rustDirs[tDir] {
				fullArgs = []string{"cargo", "test"}
				// FORCE_COLOR is a Node convention; cargo takes a flag. In JSON
				// mode the output is not printed at all, so it is left alone.
				if !jsonMode {
					fullArgs = append(fullArgs, "--color", "always")
				}
			} else if hasPackageJSON && behaviorName == "" {
				// Use `npm run test` for directories containing a package.json (Actions and Spaces).
				// This ensures package managers (npm/pnpm/yarn) naturally map their own
				// workspace resolution graphs for hoisted dependencies like @simpleplatform/sdk.
				//
				// Install only when the packages are genuinely unreachable, which
				// is the same question the comment above answers for the runner:
				// a workspace member does not carry its own node_modules, and
				// asking only for one is how a hoisted action gets a full
				// install it does not need on every run.
				_, resolvable := fsx.ResolveUpward(fsys, tDir, "node_modules")
				if !resolvable {
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
				// The runner is looked for the way an import is resolved, rather
				// than in the target's own directory and then in the one this
				// process happens to have been started from. A workspace
				// installs it at the root, which is usually neither: the walk
				// finds it, two fixed guesses did not, and missing it fell
				// through to `npx`, which resolves and may fetch on every run.
				vitestBin, found := fsx.ResolveUpward(fsys, tDir, "node_modules", ".bin", "vitest")

				if found {
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
