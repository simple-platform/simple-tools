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
	"simple-cli/internal/ui"

	"github.com/spf13/cobra"
)

var (
	deployEnv       string
	deployBump      string
	deployDryRun    bool
	deployNoInstall bool
	deployProgress  string
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

On a terminal, progress is a list of steps that updates in place. When
stdout is not a terminal, TERM is dumb, or CI is set to anything but
false or 0, each step prints plain lines instead; --progress=tty or
--progress=plain overrides the choice.

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
	deployCmd.Flags().StringVar(&deployProgress, "progress", "auto", progressFlagUsage)
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

	mode, err := progressModeFor(out, jsonOutput, deployProgress)
	if err != nil {
		return err
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
		mode:      mode,
	})
}

// deployOptions are the deploy command's arguments and flags.
type deployOptions struct {
	appPath, env, bump      string
	dryRun, noInstall, json bool
	mode                    progressMode
}

// deployDeps are deploy's side effects, replaced in tests.
type deployDeps struct {
	devops       devopsDeps
	runner       runnerDeps
	newVersioner func(parserPath string) appVersioner
	newCollector func(onProgress func(done, total int)) deploymentFileCollector
}

func defaultDeployDeps() deployDeps {
	return deployDeps{
		devops:       defaultDevopsDeps(),
		runner:       defaultRunnerDeps(),
		newVersioner: func(parserPath string) appVersioner { return deploy.NewVersionManager(parserPath) },
		newCollector: func(onProgress func(done, total int)) deploymentFileCollector {
			collector := deploy.NewFileCollector()
			collector.OnProgress = onProgress
			return collector
		},
	}
}

// appVersioner reads and bumps an app's version in its app.scl.
type appVersioner interface {
	ParseAppSCL(appPath string) (*deploy.AppSCL, error)
	BumpVersion(appPath, env, bumpType string) (string, error)
}

type deploymentFileCollector interface {
	CollectFiles(appPath string) (map[string]deploy.FileInfo, error)
}

// runDeployWith deploys opts.appPath, showing each step on out, then prints
// the result, or what a failure or interrupt left behind.
func runDeployWith(ctx context.Context, out io.Writer, deps deployDeps, opts deployOptions) error {
	start := time.Now()
	d := &deployRun{deps: deps, opts: opts, facts: &deployFacts{}}

	header := fmt.Sprintf("🚀 Deploying %s to %s", opts.appPath, opts.env)
	if opts.dryRun {
		header = fmt.Sprintf("🔍 Dry run: %s to %s (nothing is written or uploaded)", opts.appPath, opts.env)
	}
	result := stepRun{
		runner: deps.runner,
		out:    out,
		mode:   opts.mode,
		verb:   "deploy",
		header: header,
		plan:   deployPlan(opts.env, opts.dryRun),
	}.execute(ctx, d.work)

	if result.err != nil {
		return d.reportFailure(out, result)
	}
	if opts.dryRun {
		return dryRunOutput(out, d.files, d.version, opts.json)
	}
	return d.reportSuccess(out, time.Since(start))
}

// deployRun is one deploy. The fields after facts are written by the work
// and read only after a run that succeeded, which execute reports only once
// the work has returned; facts may be read at any time.
type deployRun struct {
	deps  deployDeps
	opts  deployOptions
	facts *deployFacts

	target    *devopsTarget
	versioner appVersioner
	app       *deploy.AppSCL
	version   string
	files     map[string]deploy.FileInfo
	needed    []string
	result    *deploy.DeployResult
	installed *deploy.InstallResult
}

// work runs the deploy's steps in order. The order is also a guarantee:
// app.scl is collected only after its version is bumped.
func (d *deployRun) work(ctx context.Context, steps ui.StepReporter) error {
	if err := runStep(ctx, steps, stepConfig, "", func() (string, error) { return d.loadConfig(steps) }); err != nil {
		return err
	}
	if !d.opts.dryRun {
		if err := runStep(ctx, steps, stepAuth, "", func() (string, error) { return "", d.target.authenticate(ctx) }); err != nil {
			return err
		}
	}
	if err := runStep(ctx, steps, stepVersion, "", d.bumpVersion); err != nil {
		return err
	}
	if err := runStep(ctx, steps, stepCollect, "", func() (string, error) { return d.collect(steps) }); err != nil {
		return err
	}
	if d.opts.dryRun {
		return nil
	}
	return d.upload(ctx, steps)
}

// loadConfig loads the target and reads the app ID. Both happen before
// anything is written or dialled, so a bad simple.scl or app.scl fails
// first.
func (d *deployRun) loadConfig(steps ui.StepReporter) (string, error) {
	target, err := loadDevopsTarget(d.deps.devops, d.opts.env, steps)
	if err != nil {
		return "", err
	}
	versioner := d.deps.newVersioner(target.parserPath)
	app, err := versioner.ParseAppSCL(d.opts.appPath)
	if err != nil {
		return "", err
	}
	d.target, d.versioner, d.app = target, versioner, app
	d.facts.set(func(s *deployFactsSnapshot) { s.appID = app.ID })
	return fmt.Sprintf("%s · tenant %s", app.ID, target.cfg.Tenant), nil
}

// bumpVersion writes the next version to app.scl. A dry run computes it
// with the same rules and writes nothing.
func (d *deployRun) bumpVersion() (string, error) {
	if d.opts.dryRun {
		version, err := deploy.ComputeNewVersion(d.app.Version, d.opts.env, d.opts.bump)
		if err != nil {
			return "", err
		}
		d.version = version
		return version + " · app.scl not changed", nil
	}

	// app.scl is part of the upload manifest, so it must be collected only
	// after its version has been updated. Collecting earlier would upload
	// the old app.scl under the new deployment version.
	version, err := d.versioner.BumpVersion(d.opts.appPath, d.opts.env, d.opts.bump)
	if err != nil {
		return "", err
	}
	d.version = version
	d.facts.set(func(s *deployFactsSnapshot) {
		s.version = version
		s.bumped = true
	})
	return version + " · app.scl updated", nil
}

// collect hashes and reads every deployable file.
func (d *deployRun) collect(steps ui.StepReporter) (string, error) {
	collector := d.deps.newCollector(func(done, total int) {
		steps.Progress(stepCollect, ui.Progress{Items: int64(done), ItemsTotal: int64(total), Noun: "files"})
	})
	files, err := collector.CollectFiles(d.opts.appPath)
	if err != nil {
		return "", err
	}
	d.files = files
	var size int64
	for _, fi := range files {
		size += fi.Size
	}
	return countFiles(len(files)) + " · " + ui.FormatBytes(size), nil
}

// upload connects, uploads what the server lacks, publishes and installs.
func (d *deployRun) upload(ctx context.Context, steps ui.StepReporter) error {
	client, err := connectStep(ctx, steps, d.target, d.app.ID)
	if err != nil {
		return err
	}
	defer client.Close()

	checking := fmt.Sprintf("server is checking %s against storage", countFiles(len(d.files)))
	if err := runStep(ctx, steps, stepManifest, checking, func() (string, error) {
		needed, err := client.SendManifest(ctx, d.files, d.version)
		if err != nil {
			return "", err
		}
		d.needed = needed
		return manifestSummary(newUploadPlan(d.files, needed).files, len(d.files)), nil
	}); err != nil {
		return err
	}

	plan := newUploadPlan(d.files, d.needed)
	if err := d.sendFiles(ctx, steps, client, plan); err != nil {
		return err
	}

	publishing := "server is writing the manifest"
	if plan.files > 0 {
		publishing = fmt.Sprintf("server is storing %s", countUploadedFiles(plan.files))
	}
	if err := runStep(ctx, steps, stepPublish, publishing, func() (string, error) {
		d.facts.set(func(s *deployFactsSnapshot) { s.publishSent = true })
		result, err := client.Deploy(ctx)
		if err != nil {
			return "", err
		}
		d.result = result
		d.facts.set(func(s *deployFactsSnapshot) {
			s.published = true
			if result.Version != "" {
				s.version = result.Version
			}
		})
		return result.AppID + "@" + result.Version, nil
	}); err != nil {
		return err
	}

	return d.install(ctx, steps, client)
}

// sendFiles runs the upload step, or skips it when the server has every
// file.
func (d *deployRun) sendFiles(ctx context.Context, steps ui.StepReporter, client devopsClient, plan uploadPlan) error {
	if plan.files == 0 {
		return skipStep(ctx, steps, stepUpload, "all files already on server")
	}
	return runStep(ctx, steps, stepUpload, "", func() (string, error) {
		// SendFiles reports after each acknowledgement only, so the bar
		// starts here, at zero against the totals.
		steps.Progress(stepUpload, ui.Progress{ItemsTotal: int64(plan.files), BytesTotal: plan.bytes, Noun: "files"})
		err := client.SendFiles(ctx, d.files, d.needed, func(p deploy.UploadProgress) {
			steps.Progress(stepUpload, ui.Progress{
				Items:      int64(p.FilesDone),
				ItemsTotal: int64(p.FilesTotal),
				Bytes:      p.BytesDone,
				BytesTotal: p.BytesTotal,
				Noun:       "files",
			})
		})
		if err != nil {
			return "", err
		}
		return countFiles(plan.files) + " · " + ui.FormatBytes(plan.bytes), nil
	})
}

// install runs the install step, or skips it under --no-install.
func (d *deployRun) install(ctx context.Context, steps ui.StepReporter, client devopsClient) error {
	if d.opts.noInstall {
		return skipStep(ctx, steps, stepInstall, "--no-install")
	}
	return runStep(ctx, steps, stepInstall, "running on the server (can take minutes)", func() (string, error) {
		inst := trackedInstaller{inner: client, running: func(running bool) {
			d.facts.set(func(s *deployFactsSnapshot) { s.installSent = running })
		}}
		version := d.result.Version
		installed, err := installDeployedVersion(ctx, inst, version, func(resolved string, attempt, attempts int, wait time.Duration) {
			steps.Note(stepInstall, fmt.Sprintf("↻ server resolved %s; retrying install of %s in %s (%d/%d)",
				resolved, version, ui.FormatDuration(wait), attempt, attempts))
		})
		if err != nil {
			return "", err
		}
		d.installed = installed
		return installed.Version, nil
	})
}

// reportFailure prints what a failed or interrupted deploy left behind, and
// returns the error for Execute to print.
func (d *deployRun) reportFailure(out io.Writer, result stepRunResult) error {
	facts := d.facts.snapshot()
	if d.opts.json {
		// A deploy that published but failed to install keeps its JSON
		// document on stdout, and still exits 1, as build --json does. Every
		// other failure is only the {"error"} object Execute writes to
		// stderr.
		if facts.published && !result.interrupted {
			if err := printJSONTo(out, map[string]interface{}{
				"status":  "error",
				"error":   fmt.Sprintf("deploy successful but %s: %s", installOutcome(result.err), errorSummary(result.err)),
				"app_id":  facts.appID,
				"version": facts.version,
			}); err != nil {
				return err
			}
		}
		return result.err
	}
	for _, line := range deployAftermath(facts, d.opts.env, result.interrupted, result.err) {
		_, _ = fmt.Fprintln(out, line)
	}
	return result.err
}

// reportSuccess prints the deploy's result.
func (d *deployRun) reportSuccess(out io.Writer, duration time.Duration) error {
	if d.opts.json {
		resp := map[string]interface{}{
			"status":      "success",
			"app_id":      d.result.AppID,
			"version":     d.result.Version,
			"files":       map[string]int{"total": len(d.files), "new": len(d.needed), "cached": len(d.files) - len(d.needed)},
			"duration_ms": duration.Milliseconds(),
		}
		if d.installed != nil {
			resp["installed"] = true
			resp["install_success"] = d.installed.Success
		}
		return printJSONTo(out, resp)
	}

	msg := fmt.Sprintf("✅ Deployed %s@%s", d.result.AppID, d.result.Version)
	if d.installed != nil && d.installed.Success {
		msg += " (Installed)"
	}
	_, _ = fmt.Fprintf(out, "%s in %s\n", msg, duration.Round(time.Millisecond))
	if d.opts.noInstall {
		_, _ = fmt.Fprintf(out, "   Install it with: simple install %s --env %s\n", d.result.AppID, d.opts.env)
	}
	return nil
}

// uploadPlan is what SendFiles will upload: the needed paths the collection
// has, with their total size. A needed path missing from the collection is
// skipped by SendFiles and left out here too.
type uploadPlan struct {
	files int
	bytes int64
}

func newUploadPlan(files map[string]deploy.FileInfo, needed []string) uploadPlan {
	var plan uploadPlan
	for _, path := range needed {
		if fi, ok := files[path]; ok {
			plan.files++
			plan.bytes += fi.Size
		}
	}
	return plan
}

// manifestSummary is the compare step's outcome: "312 to upload · 184
// already on server", or "nothing to upload · 496 already on server".
func manifestSummary(toUpload, total int) string {
	cached := fmt.Sprintf("%d already on server", max(total-toUpload, 0))
	if toUpload == 0 {
		return "nothing to upload · " + cached
	}
	return fmt.Sprintf("%d to upload · %s", toUpload, cached)
}

func countFiles(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

func countUploadedFiles(n int) string {
	if n == 1 {
		return "1 uploaded file"
	}
	return fmt.Sprintf("%d uploaded files", n)
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

	// No blank line first: the step lines, or the final frame's trailing
	// blank line, already set the listing apart.
	_, _ = fmt.Fprintln(out, "📋 Dry run - files to deploy:")
	for _, path := range paths {
		fi := files[path]
		_, _ = fmt.Fprintf(out, "  %s (%d bytes, hash: %s...)\n", path, fi.Size, shortHash(fi.Hash))
	}
	_, _ = fmt.Fprintf(out, "\nTotal: %d files, version: %s\n", len(files), version)
	// The listing's hashes are of the files on disk, and a dry run leaves
	// app.scl alone, so its hash is not the one a deploy would upload.
	_, _ = fmt.Fprintf(out, "app.scl is listed as it is on disk; a real deploy uploads it with version %s.\n", version)
	return nil
}

// shortHash is the first 8 characters of a content hash.
func shortHash(hash string) string {
	return hash[:min(len(hash), 8)]
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
// Any other error is returned unchanged on the first attempt. Before each
// retry, onRetry gets the version the server resolved, the number of the
// attempt that just failed, the number of attempts, and the wait. A
// cancelled ctx ends the wait, and no further install is sent.
func installDeployedVersion(ctx context.Context, inst installer, deployedVersion string,
	onRetry func(resolved string, attempt, attempts int, wait time.Duration)) (*deploy.InstallResult, error) {
	backoff := [...]time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}
	const attempts = len(backoff) + 1

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
		onRetry(match[1], attempt+1, attempts, backoff[attempt])
		if err := sleepContext(ctx, backoff[attempt]); err != nil {
			return nil, err
		}
	}

	return nil, lastErr
}

// sleepContext waits for d, or until ctx is cancelled.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
	// The timer and the cancel can be ready together, and select picks
	// either; a cancelled context must not send another install.
	return ctx.Err()
}
