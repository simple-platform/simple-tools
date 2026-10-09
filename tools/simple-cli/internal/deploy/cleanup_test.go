package deploy

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClient_Cleanup_NotJoined(t *testing.T) {
	client := &Client{timeout: 5 * time.Second}

	_, err := client.Cleanup(t.Context(), CleanupRequest{Mode: CleanupDescribe})
	if err == nil || !strings.Contains(err.Error(), "not joined") {
		t.Errorf("Cleanup() error = %v, want containing 'not joined'", err)
	}
}

// TestClient_Cleanup_Request pins the wire contract: the topic and event a
// cleanup is pushed on, and the payload of each mode.
func TestClient_Cleanup_Request(t *testing.T) {
	tests := []struct {
		name string
		req  CleanupRequest
		want map[string]any
	}{
		{
			name: "describe sends the names and no fingerprint",
			req:  CleanupRequest{Mode: CleanupDescribe, Tables: []string{"work_order", "old_thing"}, Fields: []string{"project.code"}},
			want: map[string]any{"mode": "describe", "tables": []any{"work_order", "old_thing"}, "fields": []any{"project.code"}},
		},
		{
			name: "describe leaves out a fingerprint it was given",
			req:  CleanupRequest{Mode: CleanupDescribe, Tables: []string{"old_thing"}, Expect: "ab12"},
			want: map[string]any{"mode": "describe", "tables": []any{"old_thing"}, "fields": []any{}},
		},
		{
			name: "describe leaves out a confirmation it was given",
			req:  CleanupRequest{Mode: CleanupDescribe, Tables: []string{"old_thing"}, Confirmed: CleanupConfirmedTyped},
			want: map[string]any{"mode": "describe", "tables": []any{"old_thing"}, "fields": []any{}},
		},
		{
			name: "execute sends the fingerprint it expects and that the app id was typed",
			req:  CleanupRequest{Mode: CleanupExecute, Tables: []string{"old_thing"}, Fields: []string{"project.code"}, Expect: "ab12", Confirmed: CleanupConfirmedTyped},
			want: map[string]any{"mode": "execute", "tables": []any{"old_thing"}, "fields": []any{"project.code"}, "expect": "ab12", "confirmed": "typed"},
		},
		{
			name: "execute sends that --yes confirmed it",
			req:  CleanupRequest{Mode: CleanupExecute, Tables: []string{"old_thing"}, Expect: "ab12", Confirmed: CleanupConfirmedFlag},
			want: map[string]any{"mode": "execute", "tables": []any{"old_thing"}, "fields": []any{}, "expect": "ab12", "confirmed": "flag"},
		},
		{
			name: "a missing list is sent as an empty one",
			req:  CleanupRequest{Mode: CleanupDescribe, Fields: []string{"project.code"}},
			want: map[string]any{"mode": "describe", "tables": []any{}, "fields": []any{"project.code"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu     sync.Mutex
				pushed *phoenixMessage
			)
			server := startClientMockServer(t, channelServer(func(conn *websocket.Conn, msg *phoenixMessage) bool {
				mu.Lock()
				pushed = msg
				mu.Unlock()
				writeReply(conn, msg, "ok", map[string]any{"plan": map[string]any{"app_id": "com.test.app"}})
				return true
			}))
			defer server.Close()
			client := joinedClient(t, server, 5*time.Second)

			if _, err := client.Cleanup(t.Context(), tt.req); err != nil {
				t.Fatalf("Cleanup() error = %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if pushed.Topic != "deploy:com.test.app" || pushed.Event != "cleanup" {
				t.Errorf("pushed %q on %q, want cleanup on deploy:com.test.app", pushed.Event, pushed.Topic)
			}
			if !reflect.DeepEqual(pushed.Payload, tt.want) {
				t.Errorf("payload = %v, want %v", pushed.Payload, tt.want)
			}
		})
	}
}

// TestClient_Cleanup_Replies drives a cleanup through the server's possible
// answers: a plan, a server-reported error, a reply that is not a plan, and
// silence.
func TestClient_Cleanup_Replies(t *testing.T) {
	plan := map[string]any{
		"app_id":      "com.test.app",
		"fingerprint": "ab12",
		"blocked":     true,
		"items": []any{
			map[string]any{
				"kind": "table", "name": "work_order", "in_database": true, "rows": 123,
				"metadata":   []any{map[string]any{"what": "table", "count": 1}, map[string]any{"what": "fields", "count": 10}},
				"other_apps": []any{map[string]any{"app_id": "com.test.other", "what": "db events", "count": 2}},
				"blockers":   []any{"referenced by project.sheet"},
			},
			map[string]any{
				"kind": "field", "name": "project.code", "in_database": false, "rows": 0,
				"metadata": []any{}, "blockers": []any{},
			},
		},
	}
	wantPlan := &CleanupPlan{
		AppID:       "com.test.app",
		Fingerprint: "ab12",
		Blocked:     true,
		Items: []CleanupItem{
			{
				Kind: "table", Name: "work_order", InDatabase: true, Rows: 123,
				Metadata:  []CleanupMetadata{{What: "table", Count: 1}, {What: "fields", Count: 10}},
				OtherApps: []CleanupOtherApp{{AppID: "com.test.other", What: "db events", Count: 2}},
				Blockers:  []string{"referenced by project.sheet"},
			},
			{Kind: "field", Name: "project.code", Metadata: []CleanupMetadata{}, Blockers: []string{}},
		},
	}

	tests := []struct {
		name string
		// status is the server's reply status; "" means it never answers.
		status   string
		response any
		want     *CleanupPlan
		wantErr  string
		wantIs   error
	}{
		{name: "the plan is decoded", status: "ok", response: map[string]any{"plan": plan}, want: wantPlan},
		{
			name: "an error keeps the server message", status: "error",
			response: map[string]any{"code": "stale_plan", "message": "the plan changed since it was shown"},
			wantErr:  "the plan changed since it was shown",
		},
		{
			name: "an error without a message", status: "error",
			response: map[string]any{"code": "stale_plan"},
			wantErr:  "cleanup failed",
		},
		{name: "an ok reply without a plan", status: "ok", response: map[string]any{}, wantErr: "cleanup: the reply has no plan"},
		{
			name: "a plan of the wrong shape", status: "ok",
			response: map[string]any{"plan": map[string]any{"items": []any{map[string]any{"rows": "many"}}}},
			wantErr:  "cleanup: unreadable plan",
		},
		{name: "unanswered", wantErr: "cleanup: reply timeout", wantIs: ErrReplyTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := startClientMockServer(t, channelServer(func(conn *websocket.Conn, msg *phoenixMessage) bool {
				if msg.Event == "cleanup" && tt.status != "" {
					writeReply(conn, msg, tt.status, tt.response)
				}
				return true
			}))
			defer server.Close()

			timeout := 5 * time.Second
			if tt.status == "" {
				timeout = 100 * time.Millisecond
			}
			client := joinedClient(t, server, timeout)

			got, err := client.Cleanup(t.Context(), CleanupRequest{Mode: CleanupDescribe, Tables: []string{"work_order"}})
			if tt.wantErr != "" {
				if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want starting with %q", err, tt.wantErr)
				}
				if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
					t.Errorf("error = %v, want wrapping %v", err, tt.wantIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("plan = %#v, want %#v", got, tt.want)
			}
		})
	}
}
