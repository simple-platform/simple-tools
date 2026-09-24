package cli

import (
	"context"
	"fmt"
	"io"
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
		return runInstall(cmd.Context(), cmd.OutOrStdout(), args[0])
	},
}

func init() {
	RootCmd.AddCommand(installCmd)
	installCmd.Flags().StringVar(&installEnv, "env", "", "target environment (required: dev, staging, or prod)")
	_ = installCmd.MarkFlagRequired("env")
}

// runInstall validates the command's flags and installs appID with the real
// dependencies, writing progress and results to out.
func runInstall(ctx context.Context, out io.Writer, appID string) error {
	// Validate --env flag is provided
	if installEnv == "" {
		return fmt.Errorf("--env flag is required (dev, staging, or prod)")
	}

	return runInstallWith(ctx, out, defaultDevopsDeps(), installOptions{
		appID: appID,
		env:   installEnv,
		json:  jsonOutput,
	})
}

// installOptions are the install command's arguments and flags.
type installOptions struct {
	appID, env string
	json       bool
}

// runInstallWith executes the installation logic.
// It connects to the DevOps server and requests an install for the given app ID.
func runInstallWith(ctx context.Context, out io.Writer, deps devopsDeps, opts installOptions) error {
	start := time.Now()

	// === PHASE 1: Config & Auth ===
	// Load configuration to determine where to connect (DevOps endpoint) and how to authenticate.
	notices := connectNotices{out: out, quiet: opts.json}
	target, err := loadDevopsTarget(deps, opts.env, notices)
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
	client, err := target.connect(ctx, opts.appID, notices)
	if err != nil {
		return err
	}
	defer client.Close()

	if !opts.json {
		_, _ = fmt.Fprintf(out, "🚀 Installing %s to %s...\n", opts.appID, opts.env)
	}

	// Trigger remote install process via WebSocket
	result, err := client.Install(ctx)
	if err != nil {
		return err
	}

	duration := time.Since(start)

	if opts.json {
		return printJSONTo(out, map[string]interface{}{
			"status":      "success",
			"app_id":      result.AppID,
			"version":     result.Version,
			"env":         opts.env,
			"duration_ms": duration.Milliseconds(),
		})
	}

	_, _ = fmt.Fprintf(out, "✅ Installed %s (Version: %s) to %s in %s\n", result.AppID, result.Version, opts.env, duration.Round(time.Millisecond))
	return nil
}
