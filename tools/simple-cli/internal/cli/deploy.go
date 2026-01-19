package cli

import (
	"fmt"
	"sync"
	"time"

	"simple-cli/internal/build"
	"simple-cli/internal/config"
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
		return runDeploy(fsx.OSFileSystem{}, args)
	},
}

func runDeploy(fsys fsx.FileSystem, args []string) error {
	appPath := args[0]
	start := time.Now()

	// Validate --env flag is provided
	if deployEnv == "" {
		return fmt.Errorf("--env flag is required (dev, staging, or prod)")
	}

	// Validate app exists
	if _, err := fsys.Stat(appPath); err != nil {
		return fmt.Errorf("app path '%s' not found", appPath)
	}

	// Ensure scl-parser is available (downloads if needed)
	parserPath, err := build.EnsureSCLParser(nil)
	if err != nil {
		return fmt.Errorf("failed to ensure scl-parser: %w", err)
	}

	// === PHASE 1: Config & Auth ===
	var cfg *config.SimpleSCL
	var env *config.Environment
	var cfgErr, authErr error
	var jwt string

	// Load simple.scl config
	loader := config.NewLoader(parserPath)
	cfg, cfgErr = loader.LoadSimpleSCL(".")
	if cfgErr != nil {
		return fmt.Errorf("failed to load simple.scl: %w", cfgErr)
	}

	env, cfgErr = cfg.GetEnv(deployEnv)
	if cfgErr != nil {
		return cfgErr
	}

	// Get JWT (cached for token lifetime)
	auth := deploy.NewAuthenticator()
	jwt, authErr = auth.GetJWT(env.IdentityEndpoint(), env.APIKey, deployEnv)
	if authErr != nil {
		return fmt.Errorf("authentication failed: %w", authErr)
	}

	// === PHASE 2: Version & Files (parallel) ===
	var newVersion string
	var files map[string]deploy.FileInfo
	var versionErr, filesErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		vm := deploy.NewVersionManager(parserPath)
		newVersion, versionErr = vm.BumpVersion(appPath, deployEnv, deployBump)
	}()

	go func() {
		defer wg.Done()
		collector := deploy.NewFileCollector()
		files, filesErr = collector.CollectFiles(appPath)
	}()

	wg.Wait()

	if versionErr != nil {
		return versionErr
	}
	if filesErr != nil {
		return filesErr
	}

	if !jsonOutput {
		fmt.Printf("📦 Version: %s\n", newVersion)
		fmt.Printf("📁 Files: %d\n", len(files))
	}

	if deployDryRun {
		return dryRunOutput(files, newVersion)
	}

	// === PHASE 3: Connect & Deploy ===
	client := deploy.NewClient(deploy.ClientConfig{
		Endpoint: env.DevOpsEndpoint(),
		JWT:      jwt,
		Timeout:  30 * time.Second,
	})

	if err := client.Connect(); err != nil {
		return err
	}
	defer client.Close()

	// Get app ID from app.scl
	appID, err := deploy.ExtractAppID(parserPath, appPath)
	if err != nil {
		return err
	}

	if err := client.JoinChannel(appID); err != nil {
		return err
	}

	// Send manifest
	neededFiles, err := client.SendManifest(files, newVersion)
	if err != nil {
		return err
	}

	if !jsonOutput {
		fmt.Printf("⬆️  Uploading %d files (%d cached)\n", len(neededFiles), len(files)-len(neededFiles))
	}

	// Upload needed files in parallel
	if err := client.SendFiles(files, neededFiles); err != nil {
		return err
	}

	// Trigger deploy
	result, err := client.Deploy()
	if err != nil {
		return err
	}

	// === PHASE 4: Auto-Install ===
	var installResult *deploy.InstallResult
	if !deployNoInstall {
		if !jsonOutput {
			fmt.Printf("🚀 Installing %s@%s to %s...\n", result.AppID, result.Version, deployEnv)
		}
		installResult, err = client.Install()
		if err != nil {
			// Deployment succeeded but install failed
			fmt.Printf("⚠️  Deploy successful but install failed: %v\n", err)
			// Return deployment success but with warning if this was critical?
			// For now, let's treat install failure as command failure if auto-install was requested
			if jsonOutput {
				return printJSON(map[string]interface{}{
					"status":  "error",
					"error":   fmt.Sprintf("deploy successful but install failed: %v", err),
					"app_id":  result.AppID,
					"version": result.Version,
				})
			}
			return err
		}
	}

	duration := time.Since(start)

	if jsonOutput {
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
		return printJSON(resp)
	}

	msg := fmt.Sprintf("✅ Deployed %s@%s", result.AppID, result.Version)
	if installResult != nil && installResult.Success {
		msg += " (Installed)"
	}
	fmt.Printf("%s in %s\n", msg, duration.Round(time.Millisecond))
	return nil
}

func dryRunOutput(files map[string]deploy.FileInfo, version string) error {
	if jsonOutput {
		fileList := make([]map[string]interface{}, 0, len(files))
		for path, fi := range files {
			fileList = append(fileList, map[string]interface{}{
				"path": path,
				"hash": fi.Hash,
				"size": fi.Size,
			})
		}
		return printJSON(map[string]interface{}{
			"dry_run": true,
			"version": version,
			"files":   fileList,
		})
	}

	fmt.Println("\n📋 Dry run - files to deploy:")
	for path, fi := range files {
		fmt.Printf("  %s (%d bytes, hash: %s...)\n", path, fi.Size, fi.Hash[:8])
	}
	fmt.Printf("\nTotal: %d files, version: %s\n", len(files), version)
	return nil
}

// findSCLParser is deprecated - kept for test compatibility
// Use build.EnsureSCLParser() instead which handles automatic download

func init() {
	RootCmd.AddCommand(deployCmd)
	deployCmd.Flags().StringVar(&deployEnv, "env", "", "target environment (required: dev, staging, or prod)")
	deployCmd.Flags().StringVar(&deployBump, "bump", "", "version bump type: patch|minor|major (required for first deploy after prod)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "show what would be deployed without deploying")
	deployCmd.Flags().BoolVar(&deployNoInstall, "no-install", false, "skip automatic installation after deploy")
	_ = deployCmd.MarkFlagRequired("env")
}
