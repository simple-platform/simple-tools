package cli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"simple-cli/internal/config"
	"simple-cli/internal/fsx"
)

type lintSCLParserMock struct {
	blocks []config.SCLBlock
	err    error
}

func (m lintSCLParserMock) Parse(string) ([]config.SCLBlock, error) {
	return m.blocks, m.err
}

func runSCLLintTest(t *testing.T, source string, parser config.SCLParser) []string {
	t.Helper()
	root := t.TempDir()
	appDir := filepath.Join(root, "apps", "com.acme.demo")
	writeLintFile(t, root, "apps/com.acme.demo/tables.scl", source)
	findings, err := checkSCLWorkspace(fsx.OSFileSystem{}, root, []string{appDir}, []string{appDir}, parser)
	if err != nil {
		t.Fatalf("checkSCLWorkspace() error = %v", err)
	}
	return findings
}

func TestLintSCLReportsParserErrors(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		parseErr error
		want     string
	}{
		{
			name:   "clean file parses",
			source: "table {}\n",
		},
		{
			name:     "unclosed block reports parser line and message",
			source:   "table {\n",
			parseErr: errors.New(`scl-parser failed: {"error":"unexpected end of input","line":2,"column":1}`),
			want:     "apps/com.acme.demo/tables.scl:2: unexpected end of input",
		},
		{
			name:     "parser error without a line says line is unavailable",
			source:   "table {\n",
			parseErr: errors.New("scl-parser execution failed"),
			want:     "apps/com.acme.demo/tables.scl:0: parser did not report a line: scl-parser execution failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runSCLLintTest(t, tt.source, lintSCLParserMock{err: tt.parseErr})
			got := strings.Join(findings, "\n")
			if tt.want == "" && got != "" {
				t.Fatalf("findings = %q, want none", got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("findings = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

func TestLintSCLRunsBuildRecordChecks(t *testing.T) {
	tests := []struct {
		name   string
		record string
		want   string
	}{
		{
			name:   "clean records pass",
			record: "set agent, helper {\n  status: enabled\n}\n",
		},
		{
			name:   "unfinished record marker is reported",
			record: "set agent, helper {\n  description: TODO(simple): finish this\n}\n",
			want:   "apps/com.acme.demo/records/agents.scl:2:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			appDir := filepath.Join(root, "apps", "com.acme.demo")
			writeLintFile(t, root, "apps/com.acme.demo/records/agents.scl", tt.record)
			findings, err := checkSCLWorkspace(fsx.OSFileSystem{}, root, []string{appDir}, []string{appDir}, lintSCLParserMock{})
			if err != nil {
				t.Fatalf("checkSCLWorkspace() error = %v", err)
			}
			got := strings.Join(findings, "\n")
			if tt.want == "" && got != "" {
				t.Fatalf("findings = %q, want none", got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("findings = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

func TestLintSCLReportsRepeatedNamedBlocks(t *testing.T) {
	tests := []struct {
		name   string
		source string
		blocks []config.SCLBlock
		want   string
	}{
		{
			name:   "one named block passes",
			source: "var greeting {\n  query: {}\n}\n",
			blocks: []config.SCLBlock{{Type: "block", Key: "var", Name: "greeting"}},
		},
		{
			name:   "same kind and name reports both lines",
			source: "var greeting {\n}\nvar greeting {\n}\n",
			blocks: []config.SCLBlock{
				{Type: "block", Key: "var", Name: "greeting", NameLoc: config.SCLLocation{Line: 1}},
				{Type: "block", Key: "var", Name: "greeting", NameLoc: config.SCLLocation{Line: 2}},
			},
			want: "apps/com.acme.demo/tables.scl:1:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := runSCLLintTest(t, tt.source, lintSCLParserMock{blocks: tt.blocks})
			got := strings.Join(findings, "\n")
			if tt.want == "" && got != "" {
				t.Fatalf("findings = %q, want none", got)
			}
			if tt.want != "" {
				if strings.Count(got, "apps/com.acme.demo/tables.scl:") != 2 {
					t.Fatalf("findings = %q, want both duplicate locations", got)
				}
				if !strings.Contains(got, tt.want) || !strings.Contains(got, "apps/com.acme.demo/tables.scl:3:") {
					t.Fatalf("findings = %q, want locations on lines 1 and 3", got)
				}
			}
		})
	}
}
