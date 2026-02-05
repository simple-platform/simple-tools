package cli

import (
	"fmt"
	"time"

	"simple-cli/internal/build"
	"simple-cli/internal/config"
	"simple-cli/internal/deploy"

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
		return runInstall(args[0])
	},
}

func init() {
	RootCmd.AddCommand(installCmd)
	installCmd.Flags().StringVar(&installEnv, "env", "", "target environment (required: dev, staging, or prod)")
	_ = installCmd.MarkFlagRequired("env")
}

// runInstall executes the installation logic.
// It connects to the DevOps server and requests an install for the given app ID.
func runInstall(appID string) error {
	start := time.Now()

	// Validate --env flag is provided
	if installEnv == "" {
		return fmt.Errorf("--env flag is required (dev, staging, or prod)")
	}

	// Ensure scl-parser is available (for config loading)
	// We need this to parse simple.scl to find the environment endpoints.
	parserPath, err := build.EnsureSCLParser(nil)
	if err != nil {
		return fmt.Errorf("failed to ensure scl-parser: %w", err)
	}

	// === PHASE 1: Config & Auth ===
	// Load configuration to determine where to connect (DevOps endpoint) and how to authenticate.
	var cfg *config.SimpleSCL
	var env *config.Environment
	var cfgErr, authErr error
	var jwt string

	// Load simple.scl config from current directory
	// Note: We need simple.scl for endpoints and API keys
	loader := config.NewLoader(parserPath)
	cfg, cfgErr = loader.LoadSimpleSCL(".")
	if cfgErr != nil {
		return fmt.Errorf("failed to load simple.scl: %w", cfgErr)
	}

	env, cfgErr = cfg.GetEnv(installEnv)
	if cfgErr != nil {
		return cfgErr
	}

	// Get JWT (cached for token lifetime)
	// Authentication is required to allow the CLI into the DevOps channel.
	auth := deploy.NewAuthenticator()
	jwt, authErr = auth.GetJWT(env.IdentityEndpoint(), env.APIKey, installEnv)
	if authErr != nil {
		return fmt.Errorf("authentication failed: %w", authErr)
	}

	// === PHASE 2: Connect & Install ===
	// Establish WebSocket connection to DevOps service.
	client := deploy.NewClient(deploy.ClientConfig{
		Endpoint: env.DevOpsEndpoint(),
		JWT:      jwt,
		Timeout:  30 * time.Second,
	})

	if err := client.Connect(); err != nil {
		return err
	}
	defer client.Close()

	if err := client.JoinChannel(appID); err != nil {
		return err
	}

	if !jsonOutput {
		fmt.Printf("🚀 Installing %s to %s...\n", appID, installEnv)
	}

	// Trigger remote install process via WebSocket
	result, err := client.Install()
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
