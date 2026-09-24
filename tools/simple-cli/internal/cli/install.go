package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var (
	installEnv string
)

// installCmd represents the command to install a deployed app.
// This is distinct from 'deploy'; it triggers the installation process (migrations, etc.)
// for an already uploaded artifact.
var installCmd = &cobra.Command{
	Use:   "install [APP_ID]",
	Short: "Install an app to an environment",
	Long: `Install a deployed app to the specified environment.

This command triggers the installation process (database migrations, 
service configuration, cache warming) for the latest deployed version 
of the application in the target environment.

Examples:
  simple install com.example.crm --env dev
  simple install com.example.crm --env staging
  simple install com.example.crm --env prod`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInstall(cmd.Context(), args[0])
	},
}

func init() {
	RootCmd.AddCommand(installCmd)
	installCmd.Flags().StringVar(&installEnv, "env", "", "target environment (required: dev, staging, or prod)")
	_ = installCmd.MarkFlagRequired("env")
}

// runInstall executes the installation logic.
// It connects to the DevOps server and requests an install for the given app ID.
func runInstall(ctx context.Context, appID string) error {
	start := time.Now()

	// Validate --env flag is provided
	if installEnv == "" {
		return fmt.Errorf("--env flag is required (dev, staging, or prod)")
	}

	// === PHASE 1: Config & Auth ===
	// Load configuration to determine where to connect (DevOps endpoint) and how to authenticate.
	notices := connectNotices{quiet: jsonOutput}
	target, err := loadDevopsTarget(defaultDevopsDeps(), installEnv, notices)
	if err != nil {
		return err
	}

	// Get JWT (cached for token lifetime)
	// Authentication is required to allow the CLI into the DevOps channel.
	if err := target.authenticate(ctx); err != nil {
		return err
	}

	// === PHASE 2: Connect & Install ===
	// Establish WebSocket connection to DevOps service and join the app's channel.
	client, err := target.connect(ctx, appID, notices)
	if err != nil {
		return err
	}
	defer client.Close()

	if !jsonOutput {
		fmt.Printf("🚀 Installing %s to %s...\n", appID, installEnv)
	}

	// Trigger remote install process via WebSocket
	result, err := client.Install(ctx)
	if err != nil {
		return err
	}

	duration := time.Since(start)

	if jsonOutput {
		return printJSON(map[string]interface{}{
			"status":      "success",
			"app_id":      result.AppID,
			"version":     result.Version,
			"env":         installEnv,
			"duration_ms": duration.Milliseconds(),
		})
	}

	fmt.Printf("✅ Installed %s (Version: %s) to %s in %s\n", result.AppID, result.Version, installEnv, duration.Round(time.Millisecond))
	return nil
}
