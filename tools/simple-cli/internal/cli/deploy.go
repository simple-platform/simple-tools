package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"time"

	"simple-cli/internal/deploy"
	"simple-cli/internal/fsx"

	"github.com/spf13/cobra"
)

var (
	deployEnv       string
	deployBump      string
	deployDryRun    bool
	deployNoInstall bool
)

// deployCmd represents the 'deploy' command.
// It handles the full deployment lifecycle: config loading, version bumping,
// file hashing, manifest synchronization, artifact upload, and deployment triggering.
var deployCmd = &cobra.Command{
	Use:   "deploy <app-path>",
	Short: "Deploy an app to Simple Platform",
	Long: `Deploy an app to the specified environment.

Version is automatically managed based on target environment.
Use --bump for first deploy after a prod release.
By default, the deployed version is automatically installed.
Use --no-install to skip installation (upload artifacts only).

Examples:
  simple deploy apps/com.example.crm --env dev --bump patch
  simple deploy apps/com.example.crm --env dev
  simple deploy apps/com.example.crm --env staging
  simple deploy apps/com.example.crm --env prod`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDeploy(cmd.Context(), fsx.OSFileSystem{}, cmd.OutOrStdout(), args)
	},
}

func init() {
	RootCmd.AddCommand(deployCmd)
	deployCmd.Flags().StringVar(&deployEnv, "env", "", "target environment (required: dev, staging, or prod)")
	deployCmd.Flags().StringVar(&deployBump, "bump", "", "version bump type: patch|minor|major (required for first deploy after prod)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "show what would be deployed; nothing is written or uploaded")
	deployCmd.Flags().BoolVar(&deployNoInstall, "no-install", false, "skip automatic installation after deploy")
	_ = deployCmd.MarkFlagRequired("env")
}

// runDeploy validates the command's flags and deploys args[0] with the real
// dependencies, writing progress and results to out.
func runDeploy(ctx context.Context, fsys fsx.FileSystem, out io.Writer, args []string) error {
	appPath := args[0]

	// Validate --env flag is provided
	if deployEnv == "" {
		return fmt.Errorf("--env flag is required (dev, staging, or prod)")
	}

	// Validate app exists
	if _, err := fsys.Stat(appPath); err != nil {
		return fmt.Errorf("app path '%s' not found", appPath)
	}

	return runDeployWith(ctx, out, defaultDeployDeps(), deployOptions{
		appPath:   appPath,
		env:       deployEnv,
		bump:      deployBump,
		dryRun:    deployDryRun,
		noInstall: deployNoInstall,
		json:      jsonOutput,
	})
}

// deployOptions are the deploy command's arguments and flags.
type deployOptions struct {
	appPath, env, bump      string
	dryRun, noInstall, json bool
}

// deployDeps are deploy's side effects, replaced in tests.
type deployDeps struct {
	devops       devopsDeps
	newVersioner func(parserPath string) appVersioner
	newCollector func() deploymentFileCollector
}

func defaultDeployDeps() deployDeps {
	return deployDeps{
		devops:       defaultDevopsDeps(),
		newVersioner: func(parserPath string) appVersioner { return deploy.NewVersionManager(parserPath) },
		newCollector: func() deploymentFileCollector { return deploy.NewFileCollector() },
	}
}

// runDeployWith executes the main deployment logic.
// It orchestrates local preparation and remote communication with the DevOps service.
func runDeployWith(ctx context.Context, out io.Writer, deps deployDeps, opts deployOptions) error {
	start := time.Now()

	// === PHASE 1: Config & Auth ===
	// Load configuration to determine endpoints and credentials.
	notices := connectNotices{out: out, quiet: opts.json}
	target, err := loadDevopsTarget(deps.devops, opts.env, notices)
	if err != nil {
		return err
	}

	versioner := deps.newVersioner(target.parserPath)
	if opts.dryRun {
		return dryRunDeploy(out, deps, versioner, opts)
	}

	// Get JWT (cached for token lifetime)
	if err := target.authenticate(ctx); err != nil {
		return err
	}

	// === PHASE 2: Version & Files ===
	// app.scl is part of the upload manifest, so it must be collected only
	// after its version has been updated. Parallel collection could otherwise
	// upload an old app.scl under a new deployment version.
	newVersion, files, err := prepareVersionedFiles(
		opts.appPath,
		opts.env,
		opts.bump,
		versioner,
		deps.newCollector(),
	)
	if err != nil {
		return err
	}

	if !opts.json {
		_, _ = fmt.Fprintf(out, "📦 Version: %s\n", newVersion)
		_, _ = fmt.Fprintf(out, "📁 Files: %d\n", len(files))
	}

	// === PHASE 3: Connect & Deploy ===
	// Get app ID from app.scl to verify we are deploying the correct app
	app, err := versioner.ParseAppSCL(opts.appPath)
	if err != nil {
		return err
	}

	// Establish connection to DevOps service and join the app's channel.
	client, err := target.connect(ctx, app.ID, notices)
	if err != nil {
		return err
	}
	defer client.Close()

	// Send manifest to server to check which files are missing (delta upload)
	neededFiles, err := client.SendManifest(ctx, files, newVersion)
	if err != nil {
		return err
	}

	if !opts.json {
		_, _ = fmt.Fprintf(out, "⬆️  Uploading %d files (%d cached)\n", len(neededFiles), len(files)-len(neededFiles))
	}

	// Upload needed files in parallel
	if err := client.SendFiles(ctx, files, neededFiles, nil); err != nil {
		return err
	}

	// Trigger deploy on server (finalize version)
	result, err := client.Deploy(ctx)
	if err != nil {
		return err
	}

	// === PHASE 4: Auto-Install ===
	// Optionally trigger installation immediately after successful deployment
	var installResult *deploy.InstallResult
	if !opts.noInstall {
		if !opts.json {
			_, _ = fmt.Fprintf(out, "🚀 Installing %s@%s to %s...\n", result.AppID, result.Version, opts.env)
		}
		installResult, err = installDeployedVersion(ctx, client, result.Version, func(resolved string) {
			if !opts.json {
				_, _ = fmt.Fprintf(out, "   ↻ server resolved %s; retrying install of %s…\n", resolved, result.Version)
			}
		})
		if err != nil {
			if !opts.json {
				_, _ = fmt.Fprintf(out, "⚠️  Deploy successful but install failed: %v\n", err)
				return err
			}
			// stdout carries exactly one JSON document, and the exit code
			// still says the install failed: a pipeline that only checks
			// the exit status must not read a failed install as a success.
			// This is the contract build --json follows.
			if jsonErr := printJSONTo(out, map[string]interface{}{
				"status":  "error",
				"error":   fmt.Sprintf("deploy successful but install failed: %v", err),
				"app_id":  result.AppID,
				"version": result.Version,
			}); jsonErr != nil {
				return jsonErr
			}
			return err
		}
	}

	duration := time.Since(start)

	if opts.json {
		resp := map[string]interface{}{
			"status":      "success",
			"app_id":      result.AppID,
			"version":     result.Version,
			"files":       map[string]int{"total": len(files), "new": len(neededFiles), "cached": len(files) - len(neededFiles)},
			"duration_ms": duration.Milliseconds(),
		}
		if installResult != nil {
			resp["installed"] = true
			resp["install_success"] = installResult.Success
		}
		return printJSONTo(out, resp)
	}

	msg := fmt.Sprintf("✅ Deployed %s@%s", result.AppID, result.Version)
	if installResult != nil && installResult.Success {
		msg += " (Installed)"
	}
	_, _ = fmt.Fprintf(out, "%s in %s\n", msg, duration.Round(time.Millisecond))
	return nil
}

// appVersioner reads and bumps an app's version in its app.scl.
type appVersioner interface {
	ParseAppSCL(appPath string) (*deploy.AppSCL, error)
	versionBumper
}

type versionBumper interface {
	BumpVersion(appPath, env, bumpType string) (string, error)
}

type deploymentFileCollector interface {
	CollectFiles(appPath string) (map[string]deploy.FileInfo, error)
}

func prepareVersionedFiles(
	appPath string,
	env string,
	bumpType string,
	versionBumper versionBumper,
	fileCollector deploymentFileCollector,
) (string, map[string]deploy.FileInfo, error) {
	newVersion, err := versionBumper.BumpVersion(appPath, env, bumpType)
	if err != nil {
		return "", nil, err
	}

	files, err := fileCollector.CollectFiles(appPath)
	if err != nil {
		return "", nil, err
	}

	return newVersion, files, nil
}

// dryRunDeploy shows what a deploy would upload without changing anything:
// it does not sign in, does not connect and does not write app.scl. The
// version is the one a deploy would bump to, computed from app.scl as it is.
func dryRunDeploy(out io.Writer, deps deployDeps, versioner appVersioner, opts deployOptions) error {
	app, err := versioner.ParseAppSCL(opts.appPath)
	if err != nil {
		return err
	}
	newVersion, err := deploy.ComputeNewVersion(app.Version, opts.env, opts.bump)
	if err != nil {
		return err
	}

	files, err := deps.newCollector().CollectFiles(opts.appPath)
	if err != nil {
		return err
	}

	if !opts.json {
		_, _ = fmt.Fprintf(out, "📦 Version: %s\n", newVersion)
		_, _ = fmt.Fprintf(out, "📁 Files: %d\n", len(files))
	}
	return dryRunOutput(out, files, newVersion, opts.json)
}

// dryRunOutput prints the files that would be deployed without actually deploying.
func dryRunOutput(out io.Writer, files map[string]deploy.FileInfo, version string, jsonMode bool) error {
	// Sorted, so two dry runs of the same app print the same listing and
	// can be diffed; map order would shuffle it on every run.
	paths := slices.Sorted(maps.Keys(files))
	if jsonMode {
		fileList := make([]map[string]interface{}, 0, len(files))
		for _, path := range paths {
			fi := files[path]
			fileList = append(fileList, map[string]interface{}{
				"path": path,
				"hash": fi.Hash,
				"size": fi.Size,
			})
		}
		return printJSONTo(out, map[string]interface{}{
			"dry_run": true,
			"version": version,
			"files":   fileList,
		})
	}

	_, _ = fmt.Fprintln(out, "\n📋 Dry run - files to deploy:")
	for _, path := range paths {
		fi := files[path]
		_, _ = fmt.Fprintf(out, "  %s (%d bytes, hash: %s...)\n", path, fi.Size, fi.Hash[:8])
	}
	_, _ = fmt.Fprintf(out, "\nTotal: %d files, version: %s\n", len(files), version)
	// The listing's hashes are of the files on disk, and a dry run leaves
	// app.scl alone, so its hash is not the one a deploy would upload.
	_, _ = fmt.Fprintf(out, "app.scl is listed as it is on disk; a real deploy uploads it with version %s.\n", version)
	return nil
}

// alreadyInstalledRe extracts the version named in the server's
// "Version `X` of application `Y` is already installed" reply.
var alreadyInstalledRe = regexp.MustCompile("Version `([^`]+)` of application `[^`]+` is already installed")

// installer is the part of the devops client that installs the deployed version.
type installer interface {
	Install(ctx context.Context) (*deploy.InstallResult, error)
}

// installDeployedVersion installs the version that was just deployed, absorbing
// two server behaviours that are not real failures for a deploy:
//
//   - The server reports the version we just deployed as already installed.
//     Installing is idempotent, so that outcome is a success, not an error.
//
//   - The server reports a DIFFERENT (older) version as already installed. The
//     install request carries no version, so the server resolves one itself and
//     can briefly resolve the previously installed version instead of the one
//     just deployed. Retrying resolves it once the newly deployed manifest is
//     visible, which is why a manual `simple install` immediately afterwards has
//     always succeeded.
//
// Any other error is returned unchanged on the first attempt.
func installDeployedVersion(ctx context.Context, inst installer, deployedVersion string, onRetry func(resolved string)) (*deploy.InstallResult, error) {
	const attempts = 4

	backoff := []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

	var lastErr error
	for attempt := range attempts {
		result, err := inst.Install(ctx)
		if err == nil {
			return result, nil
		}
		lastErr = err

		match := alreadyInstalledRe.FindStringSubmatch(err.Error())
		if match == nil {
			return nil, err
		}

		// The version we deployed is installed — nothing left to do.
		if match[1] == deployedVersion {
			return &deploy.InstallResult{Version: deployedVersion, Success: true}, nil
		}

		// Stale resolution: the server answered about an older version. Wait for
		// the new manifest to become visible and ask again.
		if attempt == attempts-1 {
			break
		}
		onRetry(match[1])
		time.Sleep(backoff[attempt])
	}

	return nil, lastErr
}
