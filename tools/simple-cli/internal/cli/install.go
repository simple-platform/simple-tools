package cli

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"simple-cli/internal/deploy"
	"simple-cli/internal/ui"

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

On a terminal, progress is a list of steps that updates in place. When
stdout is not a terminal, TERM is dumb, or CI is set to anything but
false or 0, each step prints plain lines instead.

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

	return runInstallWith(ctx, out, installDeps{devops: defaultDevopsDeps(), runner: defaultRunnerDeps()}, installOptions{
		appID: appID,
		env:   installEnv,
		json:  jsonOutput,
		mode:  progressModeFor(out, jsonOutput),
	})
}

// installOptions are the install command's arguments and flags.
type installOptions struct {
	appID, env string
	json       bool
	mode       progressMode
}

// installDeps are install's side effects, replaced in tests.
type installDeps struct {
	devops devopsDeps
	runner runnerDeps
}

// runInstallWith installs the latest deployed version of opts.appID,
// showing each step on out, then prints the result, or what a failure or
// interrupt may have left running.
func runInstallWith(ctx context.Context, out io.Writer, deps installDeps, opts installOptions) error {
	start := time.Now()
	var (
		// Written by the work; read only after a run that succeeded.
		result *deploy.InstallResult
		// Whether an install may be running on the server, for the lines
		// printed after a failure or an interrupt.
		installSent atomic.Bool
	)

	run := stepRun{
		runner: deps.runner,
		out:    out,
		mode:   opts.mode,
		verb:   "install",
		header: fmt.Sprintf("📥 Installing %s to %s", opts.appID, opts.env),
		plan:   installPlan(opts.env),
	}
	outcome := run.execute(ctx, func(ctx context.Context, steps ui.StepReporter) error {
		var target *devopsTarget
		if err := runStep(ctx, steps, stepConfig, "", func() (string, error) {
			t, err := loadDevopsTarget(deps.devops, opts.env, steps)
			if err != nil {
				return "", err
			}
			target = t
			return "tenant " + t.cfg.Tenant, nil
		}); err != nil {
			return err
		}

		// Authentication is required to allow the CLI into the DevOps channel.
		if err := runStep(ctx, steps, stepAuth, "", func() (string, error) { return "", target.authenticate(ctx) }); err != nil {
			return err
		}

		client, err := connectStep(ctx, steps, target, opts.appID)
		if err != nil {
			return err
		}
		defer client.Close()

		return runStep(ctx, steps, stepInstall, "running on the server (can take minutes)", func() (string, error) {
			installed, err := trackedInstaller{inner: client, running: installSent.Store}.Install(ctx)
			if err != nil {
				return "", err
			}
			result = installed
			return installed.Version, nil
		})
	})

	if outcome.err != nil {
		if !opts.json {
			for _, line := range installAftermath(opts.appID, opts.env, installSent.Load(), outcome.interrupted, outcome.err) {
				_, _ = fmt.Fprintln(out, line)
			}
		}
		return outcome.err
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
