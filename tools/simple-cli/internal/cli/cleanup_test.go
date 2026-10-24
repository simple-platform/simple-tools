package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"simple-cli/internal/config"
	"simple-cli/internal/deploy"
)

// cleanupPlanJSON is a plan as the server sends it: a table with rows and
// metadata, a field, and a table of which only metadata is left.
const cleanupPlanJSON = `{
  "app_id": "com.acme.crm",
  "fingerprint": "9f2c41d7",
  "blocked": false,
  "items": [
    {"kind": "table", "name": "work_order", "in_database": true, "rows": 123,
     "metadata": [{"what": "table", "count": 1}, {"what": "fields", "count": 10}, {"what": "relationships", "count": 3}],
     "blockers": []},
    {"kind": "field", "name": "project.code", "in_database": true, "rows": 57,
     "metadata": [{"what": "field", "count": 1}],
     "blockers": []},
    {"kind": "table", "name": "old_thing", "in_database": false, "rows": 0,
     "metadata": [{"what": "table", "count": 1}, {"what": "fields", "count": 4}],
     "blockers": []}
  ]
}`

// cleanupBlockedPlanJSON is a plan with a table that cannot be removed.
const cleanupBlockedPlanJSON = `{
  "app_id": "com.acme.crm",
  "fingerprint": "5d0e77a2",
  "blocked": true,
  "items": [
    {"kind": "table", "name": "work_order", "in_database": true, "rows": 123,
     "metadata": [{"what": "table", "count": 1}, {"what": "fields", "count": 10}],
     "blockers": ["referenced by field project.sheet", "used by the trigger on_sheet_change"]},
    {"kind": "field", "name": "project.code", "in_database": true, "rows": 1,
     "metadata": [{"what": "field", "count": 1}],
     "blockers": []}
  ]
}`

// cleanupOtherAppsPlanJSON is a plan in which a table also takes records that
// belong to other applications, and a field takes none.
const cleanupOtherAppsPlanJSON = `{
  "app_id": "com.acme.crm",
  "fingerprint": "3b8a60c5",
  "blocked": false,
  "items": [
    {"kind": "table", "name": "work_order", "in_database": true, "rows": 123,
     "metadata": [{"what": "table", "count": 1}, {"what": "fields", "count": 10}],
     "other_apps": [{"app_id": "com.acme.billing", "what": "db events", "count": 2}, {"app_id": "com.acme.reports", "what": "view", "count": 1}],
     "blockers": []},
    {"kind": "field", "name": "project.code", "in_database": true, "rows": 57,
     "metadata": [{"what": "field", "count": 1}],
     "blockers": []}
  ]
}`

func decodeCleanupPlan(t *testing.T, text string) *deploy.CleanupPlan {
	t.Helper()
	var plan deploy.CleanupPlan
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatalf("bad plan in the test: %v", err)
	}
	return &plan
}

// cleanupDocumentOf is the document --json must print for the plan in text.
func cleanupDocumentOf(t *testing.T, text string, executed bool) map[string]any {
	t.Helper()
	var plan map[string]any
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatalf("bad plan in the test: %v", err)
	}
	return map[string]any{"plan": plan, "executed": executed}
}

// cleanupFixture cleans up com.acme.crm on dev with fakes that take the time
// a real run would.
type cleanupFixture struct {
	auth    *fakeAuthenticator
	client  *fakeDevopsClient
	signals *fakeSignals
	// describe and execute answer the two kinds of request.
	describe, execute func(ctx context.Context) (*deploy.CleanupPlan, error)
	// requests are the cleanup requests the client got, in order.
	requests    []deploy.CleanupRequest
	stdin       string
	interactive bool
	ensureErr   error
	dialErr     error
	dials       int
}

func newCleanupFixture(t *testing.T, plan string) *cleanupFixture {
	f := &cleanupFixture{
		auth:    &fakeAuthenticator{wait: 900 * time.Millisecond},
		signals: &fakeSignals{},
		client:  &fakeDevopsClient{},
	}
	f.describe = func(context.Context) (*deploy.CleanupPlan, error) { return decodeCleanupPlan(t, plan), nil }
	f.execute = func(context.Context) (*deploy.CleanupPlan, error) { return decodeCleanupPlan(t, plan), nil }
	f.client.cleanup = func(ctx context.Context, req deploy.CleanupRequest) (*deploy.CleanupPlan, error) {
		f.requests = append(f.requests, req)
		if req.Mode == deploy.CleanupDescribe {
			time.Sleep(200 * time.Millisecond)
			return f.describe(ctx)
		}
		time.Sleep(1200 * time.Millisecond)
		return f.execute(ctx)
	}
	return f
}

func (f *cleanupFixture) deps() cleanupDeps {
	devops := devopsDeps{
		ensureParser: func(func(string)) (string, error) {
			time.Sleep(400 * time.Millisecond)
			if f.ensureErr != nil {
				return "", f.ensureErr
			}
			return "/fake/scl-parser", nil
		},
		loadConfig:       func(string, func(string)) (*config.SimpleSCL, error) { return testSCL(), nil },
		newAuthenticator: func() devopsAuthenticator { return f.auth },
		dial: func(context.Context, string, string) (devopsClient, error) {
			time.Sleep(300 * time.Millisecond)
			if f.dialErr != nil {
				return nil, f.dialErr
			}
			f.dials++
			return f.client, nil
		},
	}
	return cleanupDeps{
		devops:      devops,
		runner:      runnerDeps{notifySignals: f.signals.notify},
		stdin:       strings.NewReader(f.stdin),
		interactive: f.interactive,
	}
}

func (f *cleanupFixture) interruptAfter(ctx context.Context, d time.Duration) error {
	time.Sleep(d)
	f.signals.interrupt()
	<-ctx.Done()
	return ctx.Err()
}

// The transcripts of a cleanup in plain mode. The answer the person types is
// not echoed here: a terminal echoes it, with its newline.
const (
	cleanupConnected = `🔍 Checking cleanup of com.acme.crm on dev
[1/4] Load project config
[1/4] ✓ Load project config: tenant acme (0.4s)
[2/4] Authenticate
[2/4] ✓ Authenticate (0.9s)
[3/4] Connect
[3/4] ✓ Connect: devops.acme.simple.dev (0.3s)
[4/4] Check what would be removed
`
	cleanupChecked = cleanupConnected + "[4/4] ✓ Check what would be removed (0.2s)\n"

	cleanupPlanText = `
Cleanup of com.acme.crm on dev would permanently delete:

  table work_order: 123 rows
      1 table, 10 fields, 3 relationships
  field project.code: 57 rows hold a value
      1 field
  table old_thing: not in the database, leftover metadata only
      1 table, 4 fields

`
	cleanupBlockedText = `
Cleanup of com.acme.crm on dev would permanently delete:

  table work_order: 123 rows
      1 table, 10 fields
      blocked: referenced by field project.sheet
      blocked: used by the trigger on_sheet_change
  field project.code: 1 row holds a value
      1 field

`
	cleanupOtherAppsText = `
Cleanup of com.acme.crm on dev would permanently delete:

  table work_order: 123 rows
      1 table, 10 fields
      also removes from com.acme.billing: 2 db events
      also removes from com.acme.reports: 1 view
  field project.code: 57 rows hold a value
      1 field

`
	cleanupPrompt = "Type the app id (com.acme.crm) to confirm: "

	cleanupRemoving = `🧹 Cleaning up com.acme.crm on dev
[1/1] Remove from dev
`
	cleanupRemoved = cleanupRemoving + `[1/1] ✓ Remove from dev (1.2s)
✅ Removed.
   Install the version again with: simple install com.acme.crm --env dev
`
	cleanupUnknown = "The CLI stopped waiting, but the server may still be removing these from dev. Run the same command without --yes to see what is left.\n"
)

func TestRunCleanupWith(t *testing.T) {
	tables, fields := []string{"work_order", "old_thing"}, []string{"project.code"}
	// A describe changes nothing, so it carries no confirmation; an execute
	// says how the removal was confirmed.
	describeReq := deploy.CleanupRequest{Mode: deploy.CleanupDescribe, Tables: tables, Fields: fields}
	executeTyped := deploy.CleanupRequest{Mode: deploy.CleanupExecute, Tables: tables, Fields: fields, Expect: "9f2c41d7", Confirmed: deploy.CleanupConfirmedTyped}
	executeFlag := deploy.CleanupRequest{Mode: deploy.CleanupExecute, Tables: tables, Fields: fields, Expect: "9f2c41d7", Confirmed: deploy.CleanupConfirmedFlag}
	describeOnly := []deploy.CleanupRequest{describeReq}
	describeAndExecuteTyped := []deploy.CleanupRequest{describeReq, executeTyped}
	describeAndExecuteFlag := []deploy.CleanupRequest{describeReq, executeFlag}

	tests := []struct {
		name        string
		json, yes   bool
		interactive bool
		stdin       string
		// plan is the plan the server describes; empty means cleanupPlanJSON.
		plan  string
		setup func(f *cleanupFixture)
		want  string
		// wantJSON, when set, is the only thing out may hold. Under --json
		// without it, out must stay empty.
		wantJSON     map[string]any
		wantErr      string
		wantRequests []deploy.CleanupRequest
	}{
		{
			name:         "without a terminal and without --yes it only prints the plan",
			stdin:        "com.acme.crm\n",
			want:         cleanupChecked + cleanupPlanText + "Nothing was removed. To remove these, run the same command with --yes.\n",
			wantRequests: describeOnly,
		},
		{
			name:         "--yes without a terminal removes without asking",
			yes:          true,
			want:         cleanupChecked + cleanupPlanText + cleanupRemoved,
			wantRequests: describeAndExecuteFlag,
		},
		{
			name:         "--yes on a terminal does not ask either",
			yes:          true,
			interactive:  true,
			want:         cleanupChecked + cleanupPlanText + cleanupRemoved,
			wantRequests: describeAndExecuteFlag,
		},
		{
			name:         "typing the app id removes",
			interactive:  true,
			stdin:        "com.acme.crm\n",
			want:         cleanupChecked + cleanupPlanText + cleanupPrompt + cleanupRemoved,
			wantRequests: describeAndExecuteTyped,
		},
		{
			name:         "a Windows line ending after the app id removes",
			interactive:  true,
			stdin:        "com.acme.crm\r\n",
			want:         cleanupChecked + cleanupPlanText + cleanupPrompt + cleanupRemoved,
			wantRequests: describeAndExecuteTyped,
		},
		{
			name:         "another answer aborts",
			interactive:  true,
			stdin:        "yes\n",
			want:         cleanupChecked + cleanupPlanText + cleanupPrompt + "Nothing was removed.\n",
			wantErr:      "cleanup was not confirmed",
			wantRequests: describeOnly,
		},
		{
			name:         "an app id with a trailing space aborts",
			interactive:  true,
			stdin:        "com.acme.crm \n",
			want:         cleanupChecked + cleanupPlanText + cleanupPrompt + "Nothing was removed.\n",
			wantErr:      "cleanup was not confirmed",
			wantRequests: describeOnly,
		},
		{
			name:         "an app id without its line aborts",
			interactive:  true,
			stdin:        "com.acme.crm",
			want:         cleanupChecked + cleanupPlanText + cleanupPrompt + "\nNothing was removed.\n",
			wantErr:      "cleanup was not confirmed",
			wantRequests: describeOnly,
		},
		{
			name:         "a closed input aborts",
			interactive:  true,
			want:         cleanupChecked + cleanupPlanText + cleanupPrompt + "\nNothing was removed.\n",
			wantErr:      "cleanup was not confirmed",
			wantRequests: describeOnly,
		},
		{
			name:         "a blocked plan neither asks nor removes",
			yes:          true,
			interactive:  true,
			stdin:        "com.acme.crm\n",
			plan:         cleanupBlockedPlanJSON,
			want:         cleanupChecked + cleanupBlockedText + "Nothing was removed.\n",
			wantErr:      "cleanup is blocked",
			wantRequests: describeOnly,
		},
		{
			name:         "a plan names what the removal takes from other applications",
			plan:         cleanupOtherAppsPlanJSON,
			want:         cleanupChecked + cleanupOtherAppsText + "Nothing was removed. To remove these, run the same command with --yes.\n",
			wantRequests: describeOnly,
		},
		{
			name: "the server rejects the describe",
			setup: func(f *cleanupFixture) {
				f.describe = func(context.Context) (*deploy.CleanupPlan, error) {
					return nil, errors.New("no table old_thing in com.acme.crm")
				}
			},
			want:         cleanupConnected + "[4/4] ✗ Check what would be removed: no table old_thing in com.acme.crm (0.2s)\n",
			wantErr:      "no table old_thing in com.acme.crm",
			wantRequests: describeOnly,
		},
		{
			name:        "the server rejects the removal",
			interactive: true,
			stdin:       "com.acme.crm\n",
			setup: func(f *cleanupFixture) {
				f.execute = func(context.Context) (*deploy.CleanupPlan, error) {
					return nil, errors.New("the plan changed since it was shown")
				}
			},
			want:         cleanupChecked + cleanupPlanText + cleanupPrompt + cleanupRemoving + "[1/1] ✗ Remove from dev: the plan changed since it was shown (1.2s)\n",
			wantErr:      "the plan changed since it was shown",
			wantRequests: describeAndExecuteTyped,
		},
		{
			name: "the server refuses a removal confirmed with --yes",
			yes:  true,
			setup: func(f *cleanupFixture) {
				f.execute = func(context.Context) (*deploy.CleanupPlan, error) {
					return nil, errors.New("the app id must be typed to remove here")
				}
			},
			want:         cleanupChecked + cleanupPlanText + cleanupRemoving + "[1/1] ✗ Remove from dev: the app id must be typed to remove here (1.2s)\n",
			wantErr:      "the app id must be typed to remove here",
			wantRequests: describeAndExecuteFlag,
		},
		{
			name: "the connection drops during the removal",
			yes:  true,
			setup: func(f *cleanupFixture) {
				f.execute = func(context.Context) (*deploy.CleanupPlan, error) {
					return nil, fmt.Errorf("cleanup: %w: EOF", deploy.ErrConnectionLost)
				}
			},
			want: cleanupChecked + cleanupPlanText + cleanupRemoving +
				"[1/1] ✗ Remove from dev: cleanup: connection to the devops server was lost: EOF (1.2s)\n" + cleanupUnknown,
			wantErr:      "cleanup: connection to the devops server was lost: EOF",
			wantRequests: describeAndExecuteFlag,
		},
		{
			name: "interrupted during the removal",
			yes:  true,
			setup: func(f *cleanupFixture) {
				f.execute = func(ctx context.Context) (*deploy.CleanupPlan, error) {
					return nil, f.interruptAfter(ctx, 3*time.Second)
				}
			},
			want: cleanupChecked + cleanupPlanText + cleanupRemoving +
				"[1/1] ■ Remove from dev: interrupted (4.2s)\n" + cleanupUnknown,
			wantErr:      `cleanup interrupted during "Remove from dev" after 4.2s`,
			wantRequests: describeAndExecuteFlag,
		},
		{
			name: "interrupted while checking",
			setup: func(f *cleanupFixture) {
				f.describe = func(ctx context.Context) (*deploy.CleanupPlan, error) {
					return nil, f.interruptAfter(ctx, 5*time.Second)
				}
			},
			want:         cleanupConnected + "[4/4] ■ Check what would be removed: interrupted (5.2s)\n",
			wantErr:      `cleanup interrupted during "Check what would be removed" after 5.2s`,
			wantRequests: describeOnly,
		},
		{
			name:    "loading the config fails",
			setup:   func(f *cleanupFixture) { f.ensureErr = errors.New("offline") },
			want:    "🔍 Checking cleanup of com.acme.crm on dev\n[1/4] Load project config\n[1/4] ✗ Load project config: failed to ensure scl-parser: offline (0.4s)\n",
			wantErr: "failed to ensure scl-parser: offline",
		},
		{
			name:  "authentication fails",
			setup: func(f *cleanupFixture) { f.auth.errs = []error{errors.New("unreachable")} },
			want: `🔍 Checking cleanup of com.acme.crm on dev
[1/4] Load project config
[1/4] ✓ Load project config: tenant acme (0.4s)
[2/4] Authenticate
[2/4] ✗ Authenticate: authentication failed: unreachable (0.9s)
`,
			wantErr: "authentication failed: unreachable",
		},
		{
			name:  "connecting fails",
			setup: func(f *cleanupFixture) { f.dialErr = errors.New("dial tcp: no route to host") },
			want: `🔍 Checking cleanup of com.acme.crm on dev
[1/4] Load project config
[1/4] ✓ Load project config: tenant acme (0.4s)
[2/4] Authenticate
[2/4] ✓ Authenticate (0.9s)
[3/4] Connect
[3/4] ✗ Connect: dial tcp: no route to host (0.3s)
`,
			wantErr: "dial tcp: no route to host",
		},
		{
			name:         "--json only describes, and never asks",
			json:         true,
			interactive:  true,
			stdin:        "com.acme.crm\n",
			wantJSON:     cleanupDocumentOf(t, cleanupPlanJSON, false),
			wantRequests: describeOnly,
		},
		{
			// The document holds the plan the server answered the removal
			// with, not the one it described.
			name: "--json --yes removes and prints what the server removed",
			json: true,
			yes:  true,
			setup: func(f *cleanupFixture) {
				f.execute = func(context.Context) (*deploy.CleanupPlan, error) {
					removed := decodeCleanupPlan(t, cleanupPlanJSON)
					removed.Items[0].Rows = 124
					return removed, nil
				}
			},
			wantJSON:     cleanupDocumentOf(t, strings.Replace(cleanupPlanJSON, `"rows": 123`, `"rows": 124`, 1), true),
			wantRequests: describeAndExecuteFlag,
		},
		{
			name:         "--json prints a blocked plan and fails",
			json:         true,
			yes:          true,
			plan:         cleanupBlockedPlanJSON,
			wantJSON:     cleanupDocumentOf(t, cleanupBlockedPlanJSON, false),
			wantErr:      "cleanup is blocked",
			wantRequests: describeOnly,
		},
		{
			// The item that takes nothing from other applications prints no
			// other_apps key, as in the plan the server sent.
			name:         "--json keeps what the removal takes from other applications",
			json:         true,
			plan:         cleanupOtherAppsPlanJSON,
			wantJSON:     cleanupDocumentOf(t, cleanupOtherAppsPlanJSON, false),
			wantRequests: describeOnly,
		},
		{
			name: "--json prints nothing when the describe fails",
			json: true,
			setup: func(f *cleanupFixture) {
				f.describe = func(context.Context) (*deploy.CleanupPlan, error) {
					return nil, errors.New("no table old_thing in com.acme.crm")
				}
			},
			wantErr:      "no table old_thing in com.acme.crm",
			wantRequests: describeOnly,
		},
		{
			name: "--json prints nothing when the removal fails, not even what may be left",
			json: true,
			yes:  true,
			setup: func(f *cleanupFixture) {
				f.execute = func(context.Context) (*deploy.CleanupPlan, error) {
					return nil, fmt.Errorf("cleanup: %w: EOF", deploy.ErrConnectionLost)
				}
			},
			wantErr:      "cleanup: connection to the devops server was lost: EOF",
			wantRequests: describeAndExecuteFlag,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				plan := tt.plan
				if plan == "" {
					plan = cleanupPlanJSON
				}
				f := newCleanupFixture(t, plan)
				f.stdin, f.interactive = tt.stdin, tt.interactive
				if tt.setup != nil {
					tt.setup(f)
				}
				opts := cleanupOptions{appID: "com.acme.crm", env: "dev", tables: tables, fields: fields, yes: tt.yes, json: tt.json, mode: progressPlain}
				if tt.json {
					opts.mode = progressNone
				}
				var out bytes.Buffer
				err := runCleanupWith(context.Background(), &out, f.deps(), opts)

				switch {
				case tt.wantErr == "" && err != nil:
					t.Errorf("err = %v, want nil", err)
				case tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr):
					t.Errorf("err = %v, want %q", err, tt.wantErr)
				}
				if tt.wantJSON != nil {
					assertOneJSONDocument(t, out.Bytes(), tt.wantJSON)
				} else if got := out.String(); got != tt.want {
					t.Errorf("transcript:\n%s\nwant:\n%s", got, tt.want)
				}
				if !reflect.DeepEqual(f.requests, tt.wantRequests) {
					t.Errorf("requests = %+v, want %+v", f.requests, tt.wantRequests)
				}
				if f.dials > 0 {
					if got := f.client.closed.Load(); got != 1 {
						t.Errorf("client closed %d times, want 1", got)
					}
				}
			})
		})
	}
}

func TestPrintCleanupPlan(t *testing.T) {
	plan := &deploy.CleanupPlan{Items: []deploy.CleanupItem{
		{Kind: "table", Name: "note", InDatabase: true, Rows: 1},
		{Kind: "table", Name: "empty", InDatabase: true, Metadata: []deploy.CleanupMetadata{{What: "table", Count: 1}}},
		{Kind: "field", Name: "note.body", InDatabase: true, Rows: 1, Metadata: []deploy.CleanupMetadata{{What: "field", Count: 1}}},
		{Kind: "field", Name: "note.title", Rows: 9, Metadata: []deploy.CleanupMetadata{{What: "field", Count: 1}}, Blockers: []string{"used by the view note_list"}},
		// The other applications' lines come after the metadata line and
		// before the blockers, one per entry, with the label as given.
		{
			Kind: "table", Name: "tag", InDatabase: true, Rows: 4,
			Metadata:  []deploy.CleanupMetadata{{What: "table", Count: 1}},
			OtherApps: []deploy.CleanupOtherApp{{AppID: "com.acme.billing", What: "db events", Count: 2}, {AppID: "com.acme.reports", What: "view", Count: 1}},
			Blockers:  []string{"used by the trigger on_tag_change"},
		},
		{Kind: "table", Name: "bare", InDatabase: true, OtherApps: []deploy.CleanupOtherApp{{AppID: "com.acme.billing", What: "view", Count: 1}}},
		{Kind: "field", Name: "note.kind", InDatabase: true, Rows: 2, OtherApps: []deploy.CleanupOtherApp{}},
	}}
	var out bytes.Buffer
	printCleanupPlan(&out, "com.acme.crm", "dev", plan)

	want := `
Cleanup of com.acme.crm on dev would permanently delete:

  table note: 1 row
  table empty: 0 rows
      1 table
  field note.body: 1 row holds a value
      1 field
  field note.title: not in the database, leftover metadata only
      1 field
      blocked: used by the view note_list
  table tag: 4 rows
      1 table
      also removes from com.acme.billing: 2 db events
      also removes from com.acme.reports: 1 view
      blocked: used by the trigger on_tag_change
  table bare: 0 rows
      also removes from com.acme.billing: 1 view
  field note.kind: 2 rows hold a value

`
	if got := out.String(); got != want {
		t.Errorf("plan:\n%s\nwant:\n%s", got, want)
	}
}

func TestRunCleanupWith_JSONWriteFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newCleanupFixture(t, cleanupPlanJSON)
		opts := cleanupOptions{appID: "com.acme.crm", env: "dev", tables: []string{"old_thing"}, json: true, mode: progressNone}
		// The write error comes back when the document cannot be written, and
		// nothing is removed.
		if err := runCleanupWith(context.Background(), failingWriter{}, f.deps(), opts); err == nil || err.Error() != "stdout closed" {
			t.Errorf("err = %v, want the write error", err)
		}
		if len(f.requests) != 1 {
			t.Errorf("requests = %+v, want the describe only", f.requests)
		}
	})
}

func TestRunCleanup_ValidatesFlags(t *testing.T) {
	origEnv, origTables, origFields := cleanupEnv, cleanupTables, cleanupFields
	defer func() { cleanupEnv, cleanupTables, cleanupFields = origEnv, origTables, origFields }()
	tests := []struct {
		name, env string
		tables    []string
		fields    []string
		wantErr   string
	}{
		{"missing --env", "", []string{"old_thing"}, nil, "--env flag is required (dev, staging, or prod)"},
		{"nothing to remove", "dev", nil, nil, "name what to remove with at least one --table or --field"},
		{"an empty table", "dev", []string{"old_thing", ""}, nil, "--table needs a table name"},
		{"a field without a dot", "dev", nil, []string{"code"}, `--field "code" must be written table.field, with exactly one dot`},
		{"a field with two dots", "dev", []string{"old_thing"}, []string{"project.code.x"}, `--field "project.code.x" must be written table.field, with exactly one dot`},
		{"a field without a table", "dev", nil, []string{".code"}, `--field ".code" must be written table.field, with exactly one dot`},
		{"a field without a name", "dev", nil, []string{"project."}, `--field "project." must be written table.field, with exactly one dot`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanupEnv, cleanupTables, cleanupFields = tt.env, tt.tables, tt.fields
			var out bytes.Buffer
			if err := runCleanup(context.Background(), &out, "com.acme.crm"); err == nil || err.Error() != tt.wantErr {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
			if out.Len() != 0 {
				t.Errorf("a rejected command printed:\n%s", out.String())
			}
		})
	}
}

func TestCleanupCmd_FlagsRepeat(t *testing.T) {
	origEnv, origTables, origFields, origYes := cleanupEnv, cleanupTables, cleanupFields, cleanupYes
	defer func() {
		cleanupEnv, cleanupTables, cleanupFields, cleanupYes = origEnv, origTables, origFields, origYes
	}()

	args := []string{"--table", "work_order", "--field", "project.code", "--table", "old,thing", "--field", "project.name", "--yes", "--env", "dev"}
	if err := cleanupCmd.ParseFlags(args); err != nil {
		t.Fatalf("ParseFlags() error = %v", err)
	}
	// A comma is part of a name: each flag carries exactly one.
	if want := []string{"work_order", "old,thing"}; !reflect.DeepEqual(cleanupTables, want) {
		t.Errorf("--table = %q, want %q", cleanupTables, want)
	}
	if want := []string{"project.code", "project.name"}; !reflect.DeepEqual(cleanupFields, want) {
		t.Errorf("--field = %q, want %q", cleanupFields, want)
	}
	if !cleanupYes || cleanupEnv != "dev" {
		t.Errorf("--yes = %v, --env = %q, want true and dev", cleanupYes, cleanupEnv)
	}
}
