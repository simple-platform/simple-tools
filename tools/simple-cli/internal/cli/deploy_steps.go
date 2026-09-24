package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"simple-cli/internal/deploy"
	"simple-cli/internal/ui"
)

// Steps of the deploy and install plans.
const (
	stepConfig   ui.StepID = "config"
	stepAuth     ui.StepID = "auth"
	stepVersion  ui.StepID = "version"
	stepCollect  ui.StepID = "collect"
	stepConnect  ui.StepID = "connect"
	stepManifest ui.StepID = "manifest"
	stepUpload   ui.StepID = "upload"
	stepPublish  ui.StepID = "publish"
	stepInstall  ui.StepID = "install"
)

// deployPlan is every step `simple deploy` shows. A dry run neither signs
// in nor talks to the server, so its plan stops after collecting the files.
func deployPlan(env string, dryRun bool) []ui.Step {
	if dryRun {
		return []ui.Step{
			{ID: stepConfig, Title: "Load project config"},
			{ID: stepVersion, Title: "Bump version"},
			{ID: stepCollect, Title: "Collect files"},
		}
	}
	return []ui.Step{
		{ID: stepConfig, Title: "Load project config"},
		{ID: stepAuth, Title: "Authenticate"},
		{ID: stepVersion, Title: "Bump version"},
		{ID: stepCollect, Title: "Collect files"},
		{ID: stepConnect, Title: "Connect"},
		{ID: stepManifest, Title: "Compare with server"},
		{ID: stepUpload, Title: "Upload files"},
		{ID: stepPublish, Title: "Publish version"},
		{ID: stepInstall, Title: "Install to " + env},
	}
}

// installPlan is every step `simple install` shows.
func installPlan(env string) []ui.Step {
	return []ui.Step{
		{ID: stepConfig, Title: "Load project config"},
		{ID: stepAuth, Title: "Authenticate"},
		{ID: stepConnect, Title: "Connect"},
		{ID: stepInstall, Title: "Install to " + env},
	}
}

// connectStep runs the connect step: dial the devops server and join
// appID's channel. The caller closes the client it returns.
func connectStep(ctx context.Context, steps ui.StepReporter, target *devopsTarget, appID string) (devopsClient, error) {
	var client devopsClient
	err := runStep(ctx, steps, stepConnect, target.host(), func() (string, error) {
		c, err := target.connect(ctx, appID, steps)
		if err != nil {
			return "", err
		}
		client = c
		return target.host(), nil
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

// deployFacts is what a deploy has done so far, for the lines printed after
// a failure or an interrupt. The work writes it and the command reads it,
// possibly while an abandoned work still runs, so every access holds mu.
type deployFacts struct {
	mu sync.Mutex
	s  deployFactsSnapshot
}

// deployFactsSnapshot is a copy of deployFacts.
type deployFactsSnapshot struct {
	appID, version string
	// bumped: app.scl on disk now holds version.
	bumped bool
	// publishSent: the publish request was about to be sent. It is set just
	// before the request, so an interrupt in that instant errs on the side
	// of saying the server may still publish.
	publishSent bool
	// published: the server confirmed the publish.
	published bool
	// installSent: an install may be running on the server. It is cleared
	// when the server answers, so it is false while a retry waits.
	installSent bool
}

func (f *deployFacts) set(update func(*deployFactsSnapshot)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	update(&f.s)
}

func (f *deployFacts) snapshot() deployFactsSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.s
}

// outcomeUnknown reports whether err ended the wait for a request that may
// have reached the server, which may still act on it. A request that never
// left the client fails with an error that wraps none of these.
func outcomeUnknown(err error) bool {
	return errors.Is(err, deploy.ErrConnectionLost) ||
		errors.Is(err, deploy.ErrChannelClosed) ||
		errors.Is(err, deploy.ErrReplyTimeout)
}

// deployAftermath says what a deploy that failed or was interrupted left
// behind: a bumped app.scl, a publish or an install the server may still
// finish, and what to run next. It is printed in human mode only.
func deployAftermath(s deployFactsSnapshot, env string, interrupted bool, err error) []string {
	if !s.bumped {
		return nil
	}
	ref := s.appID + "@" + s.version
	install := fmt.Sprintf("simple install %s --env %s", s.appID, env)
	unknown := !interrupted && outcomeUnknown(err)

	switch {
	case s.published && interrupted && s.installSent:
		return []string{fmt.Sprintf("The server does not cancel an install when the CLI disconnects: it will keep installing %s on %s, and its result is not reported here. Let it finish before running simple install again.", ref, env)}
	case s.published && interrupted:
		return []string{fmt.Sprintf("No install is running: %s is published but not installed. Install it with: %s", ref, install)}
	case s.published:
		return []string{
			fmt.Sprintf("⚠️  Deploy successful but %s: %s", installOutcome(err), errorSummary(err)),
			installNextStep(ref, install, unknown),
		}
	case s.publishSent && interrupted:
		// The request may not have left, and the server can still reject
		// it, so the publish is only possible.
		return []string{fmt.Sprintf("The server does not cancel a publish when the CLI disconnects: %s may still be published, but it will not be installed. If it is, install it with: %s", ref, install)}
	case s.publishSent && unknown:
		return []string{fmt.Sprintf("The CLI stopped waiting, but the server may still finish publishing %s. If it does, install it with: %s", ref, install)}
	case s.publishSent:
		return []string{fmt.Sprintf("%s was not published. app.scl was already bumped to %s on disk.", ref, s.version)}
	default:
		return []string{fmt.Sprintf("Nothing was published. app.scl was already bumped to %s on disk.", s.version)}
	}
}

// installOutcome names how the install after a publish ended: "install
// failed" when the server answered, or "the install result is unknown"
// when the wait for its answer ended without one.
func installOutcome(err error) string {
	if outcomeUnknown(err) {
		return "the install result is unknown"
	}
	return "install failed"
}

// installNextStep is the line after the install outcome: what to run, and
// whether to wait for an install the server may still be running.
func installNextStep(ref, install string, unknown bool) string {
	if unknown {
		return fmt.Sprintf("   The CLI stopped waiting, but the server may still be installing %s. Let it finish before retrying with: %s", ref, install)
	}
	return fmt.Sprintf("   %s is published. Retry the install with: %s", ref, install)
}

// errorSummary is the first line of err. A recovered panic's error carries
// its stack after that line; the stack belongs on stderr, where Execute
// prints the whole error, not in the lines printed on stdout.
func errorSummary(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

// installAftermath says what a `simple install` that failed or was
// interrupted may have left running on the server. It is printed in human
// mode only.
func installAftermath(appID, env string, installSent, interrupted bool, err error) []string {
	switch {
	case !installSent:
		return nil
	case interrupted:
		return []string{fmt.Sprintf("The server does not cancel an install when the CLI disconnects: it will keep installing %s on %s, and its result is not reported here. Let it finish before running simple install again.", appID, env)}
	case outcomeUnknown(err):
		return []string{fmt.Sprintf("The CLI stopped waiting, but the server may still be installing %s on %s. Let it finish before running simple install again.", appID, env)}
	default:
		return nil
	}
}

// trackedInstaller reports, through running, whether an install may be in
// progress on the server: from just before the request until the server
// answers it. A wait that ended without an answer (a dropped connection, a
// timeout or an interrupt) leaves it set, because the server may still be
// installing.
type trackedInstaller struct {
	inner   installer
	running func(bool)
}

// Install implements installer.
func (i trackedInstaller) Install(ctx context.Context) (*deploy.InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	i.running(true)
	result, err := i.inner.Install(ctx)
	if err == nil || (!outcomeUnknown(err) && ctx.Err() == nil) {
		i.running(false)
	}
	return result, err
}
