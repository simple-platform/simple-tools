package cli

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"simple-cli/internal/deploy"
	"simple-cli/internal/ui"
)

func planTitles(plan []ui.Step) []string {
	titles := make([]string, len(plan))
	for i, step := range plan {
		titles[i] = string(step.ID) + ":" + step.Title
	}
	return titles
}

func TestDeployPlan(t *testing.T) {
	tests := []struct {
		name   string
		env    string
		dryRun bool
		want   []string
	}{
		{
			name: "deploy",
			env:  "staging",
			want: []string{
				"config:Load project config", "auth:Authenticate", "version:Bump version", "collect:Collect files",
				"connect:Connect", "manifest:Compare with server", "upload:Upload files", "publish:Publish version",
				"install:Install to staging",
			},
		},
		{
			name:   "a dry run neither signs in nor connects",
			env:    "dev",
			dryRun: true,
			want:   []string{"config:Load project config", "version:Bump version", "collect:Collect files"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := planTitles(deployPlan(tt.env, tt.dryRun)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deployPlan() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstallPlan(t *testing.T) {
	want := []string{"config:Load project config", "auth:Authenticate", "connect:Connect", "install:Install to prod"}
	if got := planTitles(installPlan("prod")); !reflect.DeepEqual(got, want) {
		t.Errorf("installPlan() = %q, want %q", got, want)
	}
}

func TestOutcomeUnknown(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"connection lost", fmt.Errorf("install: %w: EOF", deploy.ErrConnectionLost), true},
		{"channel closed", fmt.Errorf("deploy: %w (phx_error)", deploy.ErrChannelClosed), true},
		{"reply timeout", fmt.Errorf("install: %w after 15m0s", deploy.ErrReplyTimeout), true},
		// A request refused before it left names its cause with %v, so it
		// wraps no sentinel: the server cannot have acted on it.
		{"never sent", fmt.Errorf("install: request not sent: %v", deploy.ErrConnectionLost), false},
		{"server said no", errors.New("record sync failed"), false},
		{"cancelled", context.Canceled, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outcomeUnknown(tt.err); got != tt.want {
				t.Errorf("outcomeUnknown(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestDeployAftermath(t *testing.T) {
	const (
		env     = "dev"
		ref     = "com.acme.crm@1.4.3-dev.5"
		install = "simple install com.acme.crm --env dev"
	)
	definite := errors.New("record sync failed")
	unknown := fmt.Errorf("install: %w: EOF", deploy.ErrConnectionLost)
	panicked := &workPanic{value: "boom", stack: []byte("goroutine 1 [running]:\nmain.main()")}
	facts := func(publishSent, published, installSent bool) deployFactsSnapshot {
		return deployFactsSnapshot{
			appID: "com.acme.crm", version: "1.4.3-dev.5", bumped: true,
			publishSent: publishSent, published: published, installSent: installSent,
		}
	}
	tests := []struct {
		name        string
		facts       deployFactsSnapshot
		interrupted bool
		err         error
		want        []string
	}{
		{name: "not bumped: interrupted", facts: deployFactsSnapshot{appID: "com.acme.crm"}, interrupted: true, err: errInterrupted},
		{name: "not bumped: unknown", facts: deployFactsSnapshot{appID: "com.acme.crm"}, err: unknown},
		{name: "not bumped: definite", facts: deployFactsSnapshot{appID: "com.acme.crm"}, err: definite},
		{
			name: "bumped: interrupted", facts: facts(false, false, false), interrupted: true, err: errInterrupted,
			want: []string{"Nothing was published. app.scl was already bumped to 1.4.3-dev.5 on disk."},
		},
		{
			name: "bumped: unknown", facts: facts(false, false, false), err: unknown,
			want: []string{"Nothing was published. app.scl was already bumped to 1.4.3-dev.5 on disk."},
		},
		{
			name: "bumped: definite", facts: facts(false, false, false), err: definite,
			want: []string{"Nothing was published. app.scl was already bumped to 1.4.3-dev.5 on disk."},
		},
		{
			name: "publish sent: interrupted", facts: facts(true, false, false), interrupted: true, err: errInterrupted,
			want: []string{"The server does not cancel a publish when the CLI disconnects: " + ref + " may still be published, but it will not be installed. If it is, install it with: " + install},
		},
		{
			name: "publish sent: unknown", facts: facts(true, false, false), err: unknown,
			want: []string{"The CLI stopped waiting, but the server may still finish publishing " + ref + ". If it does, install it with: " + install},
		},
		{
			name: "publish sent: definite", facts: facts(true, false, false), err: definite,
			want: []string{ref + " was not published. app.scl was already bumped to 1.4.3-dev.5 on disk."},
		},
		{
			name: "install in flight: interrupted", facts: facts(true, true, true), interrupted: true, err: errInterrupted,
			want: []string{"The server does not cancel an install when the CLI disconnects: it will keep installing " + ref + " on dev, and its result is not reported here. Let it finish before running simple install again."},
		},
		{
			name: "install in flight: unknown", facts: facts(true, true, true), err: unknown,
			want: []string{
				"⚠️  Deploy successful but the install result is unknown: install: connection to the devops server was lost: EOF",
				"   The CLI stopped waiting, but the server may still be installing " + ref + ". Let it finish before retrying with: " + install,
			},
		},
		{
			name: "install answered: definite", facts: facts(true, true, false), err: definite,
			want: []string{
				"⚠️  Deploy successful but install failed: record sync failed",
				"   " + ref + " is published. Retry the install with: " + install,
			},
		},
		{
			name: "install panicked: only the first line", facts: facts(true, true, true), err: panicked,
			want: []string{
				"⚠️  Deploy successful but install failed: internal error: boom",
				"   " + ref + " is published. Retry the install with: " + install,
			},
		},
		{
			name: "no install in flight: interrupted", facts: facts(true, true, false), interrupted: true, err: errInterrupted,
			want: []string{"No install is running: " + ref + " is published but not installed. Install it with: " + install},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deployAftermath(tt.facts, env, tt.interrupted, tt.err); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deployAftermath() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestErrorSummary(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"one line", errors.New("record sync failed"), "record sync failed"},
		{"a panic's stack is dropped", &workPanic{value: "boom", stack: []byte("goroutine 1 [running]:")}, "internal error: boom"},
		{"a server message's later lines are dropped", errors.New("install failed:\n  field owner"), "install failed:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorSummary(tt.err); got != tt.want {
				t.Errorf("errorSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstallOutcome(t *testing.T) {
	if got := installOutcome(errors.New("record sync failed")); got != "install failed" {
		t.Errorf("an answered install: %q", got)
	}
	if got := installOutcome(fmt.Errorf("install: %w after 15m0s", deploy.ErrReplyTimeout)); got != "the install result is unknown" {
		t.Errorf("an unanswered install: %q", got)
	}
}

func TestInstallAftermath(t *testing.T) {
	unknown := fmt.Errorf("install: %w after 15m0s", deploy.ErrReplyTimeout)
	tests := []struct {
		name        string
		installSent bool
		interrupted bool
		err         error
		want        []string
	}{
		{name: "failed before the install was sent", err: errors.New("authentication failed: 401")},
		{name: "interrupted before the install was sent", interrupted: true, err: errInterrupted},
		{
			name: "interrupted while installing", installSent: true, interrupted: true, err: errInterrupted,
			want: []string{"The server does not cancel an install when the CLI disconnects: it will keep installing com.acme.crm on dev, and its result is not reported here. Let it finish before running simple install again."},
		},
		{
			name: "stopped waiting", installSent: true, err: unknown,
			want: []string{"The CLI stopped waiting, but the server may still be installing com.acme.crm on dev. Let it finish before running simple install again."},
		},
		{name: "the server answered", installSent: true, err: errors.New("record sync failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := installAftermath("com.acme.crm", "dev", tt.installSent, tt.interrupted, tt.err); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("installAftermath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTrackedInstaller(t *testing.T) {
	tests := []struct {
		name         string
		cancelBefore bool
		cancelDuring bool
		err          error
		wantCalled   bool
		wantRunning  bool
	}{
		{name: "installed", wantCalled: true},
		{name: "the server said no", err: errors.New("record sync failed"), wantCalled: true},
		{name: "never sent", err: fmt.Errorf("install: request not sent: %v", deploy.ErrConnectionLost), wantCalled: true},
		{name: "the connection dropped", err: fmt.Errorf("install: %w: EOF", deploy.ErrConnectionLost), wantCalled: true, wantRunning: true},
		{name: "interrupted while waiting", cancelDuring: true, err: context.Canceled, wantCalled: true, wantRunning: true},
		{name: "interrupted before sending", cancelBefore: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelBefore {
				cancel()
			}
			var marks []bool
			called := false
			inst := trackedInstaller{
				inner: installerFunc(func(context.Context) (*deploy.InstallResult, error) {
					called = true
					if tt.cancelDuring {
						cancel()
					}
					if tt.err != nil {
						return nil, tt.err
					}
					return &deploy.InstallResult{Version: "1.0.0", Success: true}, nil
				}),
				running: func(running bool) { marks = append(marks, running) },
			}
			_, err := inst.Install(ctx)
			if called != tt.wantCalled {
				t.Errorf("inner called = %v, want %v", called, tt.wantCalled)
			}
			if tt.cancelBefore && !errors.Is(err, context.Canceled) {
				t.Errorf("err = %v, want context.Canceled", err)
			}
			running := len(marks) > 0 && marks[len(marks)-1]
			if running != tt.wantRunning {
				t.Errorf("running marks = %v, want the install left running = %v", marks, tt.wantRunning)
			}
			if tt.wantCalled && (len(marks) == 0 || !marks[0]) {
				t.Errorf("running marks = %v, want true before the request", marks)
			}
		})
	}
}

func TestDeployFacts(t *testing.T) {
	var facts deployFacts
	facts.set(func(s *deployFactsSnapshot) { s.appID, s.bumped = "com.acme.crm", true })
	snap := facts.snapshot()
	facts.set(func(s *deployFactsSnapshot) { s.published = true })
	if snap.published || !facts.snapshot().published || snap.appID != "com.acme.crm" {
		t.Errorf("snapshot is not a copy: %+v then %+v", snap, facts.snapshot())
	}
}

func TestConnectStep(t *testing.T) {
	client := &fakeDevopsClient{}
	var dials []string
	target, err := loadDevopsTarget(fakeDevopsDeps(&fakeAuthenticator{}, &dials, func(int) (devopsClient, error) {
		return client, nil
	}), "dev", ui.NopReporter{}, nil)
	if err != nil {
		t.Fatal(err)
	}

	steps := &recordingReporter{}
	got, err := connectStep(context.Background(), steps, target, "com.acme.crm")
	if err != nil || got != client {
		t.Fatalf("connectStep() = %v, %v", got, err)
	}
	want := []string{"start connect devops.acme.simple.dev", "done connect devops.acme.simple.dev"}
	if !reflect.DeepEqual(steps.got(), want) {
		t.Errorf("reports = %q, want %q", steps.got(), want)
	}

	client.join = func(context.Context, string) error { return errors.New("join error: unauthorized") }
	if got, err := connectStep(context.Background(), &recordingReporter{}, target, "com.acme.crm"); err == nil || got != nil {
		t.Errorf("connectStep() = %v, %v; want the join error and no client", got, err)
	}
}
