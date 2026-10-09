package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"simple-cli/internal/deploy"
	"simple-cli/internal/ui"

	"github.com/spf13/cobra"
)

var (
	cleanupEnv    string
	cleanupTables []string
	cleanupFields []string
	cleanupYes    bool
)

// cleanupCmd represents the command to delete tables and fields on purpose.
// An install refuses a version that would drop either, so this is the
// separate, explicit step that does it.
var cleanupCmd = &cobra.Command{
	Use:   "cleanup <APP_ID>",
	Short: "Permanently delete tables and fields from an app",
	Long: `Permanently delete the tables and fields you name, and their data, from an
app in the specified environment.

An install refuses a version that would drop a table or a field. This is the
separate step that removes them on purpose: you name exactly what to remove,
see everything that will be deleted, and confirm by typing the app id. A plan
with blocked items removes nothing. Afterwards, install the version again.

Without a terminal on stdin the command only prints the plan, and --yes
removes without asking. With --json it never asks: it prints the plan, or
with --yes removes and prints what was removed.

Examples:
  simple cleanup com.example.crm --table old_thing --env dev
  simple cleanup com.example.crm --field project.code --env staging
  simple cleanup com.example.crm --table old_thing --field project.code --env dev --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runCleanup(cmd.Context(), cmd.OutOrStdout(), args[0])
	},
}

func init() {
	RootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().StringVar(&cleanupEnv, "env", "", "target environment (required: dev, staging, or prod)")
	cleanupCmd.Flags().StringArrayVar(&cleanupTables, "table", nil, "table to remove (repeatable)")
	cleanupCmd.Flags().StringArrayVar(&cleanupFields, "field", nil, "field to remove, as table.field (repeatable)")
	cleanupCmd.Flags().BoolVar(&cleanupYes, "yes", false, "remove without asking for confirmation")
	_ = cleanupCmd.MarkFlagRequired("env")
}

// Steps of cleanup's plans, beside the connection steps it shares with
// install.
const (
	stepDescribe ui.StepID = "describe"
	stepCleanup  ui.StepID = "cleanup"
)

// A cleanup that removes nothing and exits 1 returns one of these after
// printing why.
var (
	errCleanupBlocked  = errors.New("cleanup is blocked")
	errCleanupDeclined = errors.New("cleanup was not confirmed")
)

// runCleanup validates the command's flags and cleans up appID with the real
// dependencies, writing progress and results to out.
func runCleanup(ctx context.Context, out io.Writer, appID string) error {
	// Validate --env flag is provided
	if cleanupEnv == "" {
		return fmt.Errorf("--env flag is required (dev, staging, or prod)")
	}
	if err := checkCleanupTargets(cleanupTables, cleanupFields); err != nil {
		return err
	}

	mode, err := progressModeFor(out, jsonOutput, "auto")
	if err != nil {
		return err
	}

	return runCleanupWith(ctx, out, defaultCleanupDeps(), cleanupOptions{
		appID:  appID,
		env:    cleanupEnv,
		tables: cleanupTables,
		fields: cleanupFields,
		yes:    cleanupYes,
		json:   jsonOutput,
		mode:   mode,
	})
}

// checkCleanupTargets checks the names given with --table and --field before
// anything is sent: at least one, no empty table, and every field written
// table.field with exactly one dot.
func checkCleanupTargets(tables, fields []string) error {
	if len(tables) == 0 && len(fields) == 0 {
		return errors.New("name what to remove with at least one --table or --field")
	}
	for _, table := range tables {
		if table == "" {
			return errors.New("--table needs a table name")
		}
	}
	for _, field := range fields {
		table, name, ok := strings.Cut(field, ".")
		if !ok || table == "" || name == "" || strings.Contains(name, ".") {
			return fmt.Errorf("--field %q must be written table.field, with exactly one dot", field)
		}
	}
	return nil
}

// cleanupOptions are the cleanup command's arguments and flags.
type cleanupOptions struct {
	appID, env     string
	tables, fields []string
	yes, json      bool
	mode           progressMode
}

// cleanupDeps are cleanup's side effects, replaced in tests.
type cleanupDeps struct {
	devops devopsDeps
	runner runnerDeps
	// stdin is where the confirmation is read from. interactive says that a
	// person is typing there: without one, cleanup never asks.
	stdin       io.Reader
	interactive bool
}

func defaultCleanupDeps() cleanupDeps {
	return cleanupDeps{
		devops:      defaultDevopsDeps(),
		runner:      defaultRunnerDeps(),
		stdin:       os.Stdin,
		interactive: progressInputIsTerminal(os.Stdin),
	}
}

// runCleanupWith asks the server what removing opts.tables and opts.fields
// would delete, shows the plan, and removes them once that is confirmed. It
// is two stepRuns around the confirmation, one up to the plan and one for the
// removal: the live view holds the terminal's input, so the question cannot
// be asked inside either.
func runCleanupWith(ctx context.Context, out io.Writer, deps cleanupDeps, opts cleanupOptions) error {
	var (
		// Written by the work; read only after a run that succeeded.
		client devopsClient
		plan   *deploy.CleanupPlan
	)

	check := stepRun{
		runner: deps.runner,
		out:    out,
		mode:   opts.mode,
		verb:   "cleanup",
		header: fmt.Sprintf("🔍 Checking cleanup of %s on %s", opts.appID, opts.env),
		plan: []ui.Step{
			{ID: stepConfig, Title: "Load project config"},
			{ID: stepAuth, Title: "Authenticate"},
			{ID: stepConnect, Title: "Connect"},
			{ID: stepDescribe, Title: "Check what would be removed"},
		},
	}
	outcome := check.execute(ctx, func(ctx context.Context, steps ui.StepReporter) error {
		var target *devopsTarget
		if err := runStep(ctx, steps, stepConfig, "", func() (string, error) {
			t, err := loadDevopsTarget(deps.devops, opts.env, steps, configWarning(opts.mode, steps))
			if err != nil {
				return "", err
			}
			target = t
			return "tenant " + t.cfg.Tenant, nil
		}); err != nil {
			return err
		}

		if err := runStep(ctx, steps, stepAuth, "", func() (string, error) { return "", target.authenticate(ctx) }); err != nil {
			return err
		}

		c, err := connectStep(ctx, steps, target, opts.appID)
		if err != nil {
			return err
		}

		var described *deploy.CleanupPlan
		if err := runStep(ctx, steps, stepDescribe, "", func() (string, error) {
			var err error
			described, err = c.Cleanup(ctx, deploy.CleanupRequest{Mode: deploy.CleanupDescribe, Tables: opts.tables, Fields: opts.fields})
			return "", err
		}); err != nil {
			c.Close()
			return err
		}
		client, plan = c, described
		return nil
	})
	if outcome.err != nil {
		return outcome.err
	}
	defer client.Close()

	confirmed, err := reviewCleanup(out, deps, opts, plan)
	if confirmed == "" {
		return err
	}

	remove := stepRun{
		runner: deps.runner,
		out:    out,
		mode:   opts.mode,
		verb:   "cleanup",
		header: fmt.Sprintf("🧹 Cleaning up %s on %s", opts.appID, opts.env),
		plan:   []ui.Step{{ID: stepCleanup, Title: "Remove from " + opts.env}},
	}
	var removed *deploy.CleanupPlan
	outcome = remove.execute(ctx, func(ctx context.Context, steps ui.StepReporter) error {
		return runStep(ctx, steps, stepCleanup, "running on the server (can take minutes)", func() (string, error) {
			p, err := client.Cleanup(ctx, deploy.CleanupRequest{Mode: deploy.CleanupExecute, Tables: opts.tables, Fields: opts.fields, Expect: plan.Fingerprint, Confirmed: confirmed})
			if err != nil {
				return "", err
			}
			removed = p
			return "", nil
		})
	})
	if outcome.err != nil {
		// A wait that ended without an answer leaves it unknown whether the
		// server still removes. A failure it answered removed nothing.
		if !opts.json && (outcome.interrupted || outcomeUnknown(outcome.err)) {
			_, _ = fmt.Fprintf(out, "The CLI stopped waiting, but the server may still be removing these from %s. Run the same command without --yes to see what is left.\n", opts.env)
		}
		return outcome.err
	}

	if opts.json {
		return printJSONTo(out, cleanupDocument(removed, true))
	}
	_, _ = fmt.Fprintln(out, "✅ Removed.")
	_, _ = fmt.Fprintf(out, "   Install the version again with: simple install %s --env %s\n", opts.appID, opts.env)
	return nil
}

// reviewCleanup shows plan and decides whether the removal goes on, asking
// for the app id when it has to. It returns how the removal was confirmed,
// deploy.CleanupConfirmedTyped or deploy.CleanupConfirmedFlag, or "" when it
// does not go on. Then nothing was removed: the error, if any, is why the
// command exits 1, and nil means it only waits for --yes.
func reviewCleanup(out io.Writer, deps cleanupDeps, opts cleanupOptions, plan *deploy.CleanupPlan) (confirmed string, err error) {
	if opts.json {
		// --json never asks, so a plan runs only when --yes says so.
		if opts.yes && !plan.Blocked {
			return deploy.CleanupConfirmedFlag, nil
		}
		if err := printJSONTo(out, cleanupDocument(plan, false)); err != nil {
			return "", err
		}
		if plan.Blocked {
			return "", errCleanupBlocked
		}
		return "", nil
	}

	printCleanupPlan(out, opts.appID, opts.env, plan)
	switch {
	case plan.Blocked:
		_, _ = fmt.Fprintln(out, "Nothing was removed.")
		return "", errCleanupBlocked
	case opts.yes:
		return deploy.CleanupConfirmedFlag, nil
	case !deps.interactive:
		_, _ = fmt.Fprintln(out, "Nothing was removed. To remove these, run the same command with --yes.")
		return "", nil
	case !typedAppID(out, deps.stdin, opts.appID):
		_, _ = fmt.Fprintln(out, "Nothing was removed.")
		return "", errCleanupDeclined
	}
	return deploy.CleanupConfirmedTyped, nil
}

// typedAppID asks for appID to be typed, and reports whether the answer was
// exactly that on a line of its own. A closed input is a no.
func typedAppID(out io.Writer, in io.Reader, appID string) bool {
	_, _ = fmt.Fprintf(out, "Type the app id (%s) to confirm: ", appID)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		// No Enter ended the line, so end it here.
		_, _ = fmt.Fprintln(out)
		return false
	}
	return strings.TrimRight(line, "\r\n") == appID
}

// cleanupDocument is the one JSON document --json prints.
func cleanupDocument(plan *deploy.CleanupPlan, executed bool) map[string]interface{} {
	return map[string]interface{}{"plan": plan, "executed": executed}
}

// printCleanupPlan lists what plan deletes: each item with what it holds,
// the metadata that goes with it, what it also takes from other applications
// and anything that blocks it.
func printCleanupPlan(out io.Writer, appID, env string, plan *deploy.CleanupPlan) {
	_, _ = fmt.Fprintf(out, "\nCleanup of %s on %s would permanently delete:\n\n", appID, env)
	for _, item := range plan.Items {
		_, _ = fmt.Fprintf(out, "  %s %s: %s\n", item.Kind, item.Name, cleanupItemSummary(item))
		if len(item.Metadata) > 0 {
			counts := make([]string, len(item.Metadata))
			for i, m := range item.Metadata {
				counts[i] = fmt.Sprintf("%d %s", m.Count, m.What)
			}
			_, _ = fmt.Fprintf(out, "      %s\n", strings.Join(counts, ", "))
		}
		for _, other := range item.OtherApps {
			_, _ = fmt.Fprintf(out, "      also removes from %s: %d %s\n", other.AppID, other.Count, other.What)
		}
		for _, blocker := range item.Blockers {
			_, _ = fmt.Fprintf(out, "      blocked: %s\n", blocker)
		}
	}
	_, _ = fmt.Fprintln(out)
}

// cleanupItemSummary is what an item holds: its rows, or that it is gone
// from the database and only its metadata is left.
func cleanupItemSummary(item deploy.CleanupItem) string {
	if !item.InDatabase {
		return "not in the database, leftover metadata only"
	}
	rows, holds := fmt.Sprintf("%d rows", item.Rows), "hold"
	if item.Rows == 1 {
		rows, holds = "1 row", "holds"
	}
	if item.Kind == "field" {
		return rows + " " + holds + " a value"
	}
	return rows
}
