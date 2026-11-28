package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"simple-cli/internal/build"
	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"
	"simple-cli/internal/testsupport"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Everything `simple init` and `simple new ...` write must pass the linter a
// new workspace ships with, as written: no finding, and nothing left for an
// auto-fix or a commit hook to correct. A freshly generated workspace once
// had 58 errors and 3 warnings in ten files.
//
// Reading the templates cannot show that. A template is not the file a
// developer gets: names are filled in, some files are assembled by a command,
// and a Markdown table or a TOML line that is laid out correctly for one name
// is not for a longer one. So this runs the commands themselves, once with
// short names and once with long ones, creates one of every kind, and runs the
// shipped linter over the result.
//
// The workspace is written under this module, in tmp, for the reason given in
// scaffold's TestGeneratedBehaviorTestPassesWithVitest: the generated
// eslint.config.js imports the lint packages, and Node finds a package by
// walking up from the importing file.
func TestGeneratedWorkspacePassesLint(t *testing.T) {
	// What this test sets on the commands must not reach the tests after it.
	t.Cleanup(func() { resetCommandTree(t) })

	tests := []struct {
		name, projectName, appID                                                    string
		action, rustAction, bothAction, tool                                        string
		rustTool, space, behavior, agent, taskType, agentTaskType, skill, knowledge string
		dbTrigger, timedTrigger, webhookTrigger                                     string
	}{
		{
			name:           "short names",
			projectName:    "acme",
			appID:          "com.acme.demo",
			action:         "hi",
			rustAction:     "hello",
			bothAction:     "both",
			tool:           "lookup-order",
			rustTool:       "summarize-csv",
			space:          "board",
			behavior:       "orders",
			agent:          "support",
			taskType:       "returns",
			agentTaskType:  "triage-requests",
			skill:          "refund-policy",
			knowledge:      "return-terms",
			dbTrigger:      "order-created",
			timedTrigger:   "daily-report",
			webhookTrigger: "order-webhook",
		},
		{
			name:           "long names",
			projectName:    "acme-industrial-holdings",
			appID:          "com.acme-industrial.field-service-management",
			action:         "escalate-overdue-work-order",
			rustAction:     "dispatch-industrial-service-technician",
			bothAction:     "handle-field-service-escalation",
			tool:           "lookup-industrial-work-order",
			rustTool:       "summarize-industrial-maintenance-history",
			space:          "field-service-management-dashboard",
			behavior:       "industrial_work_orders",
			agent:          "service-dispatch-coordinator",
			taskType:       "field-service-operations",
			agentTaskType:  "industrial-triage-operations",
			skill:          "escalate-overdue-work-order",
			knowledge:      "industrial-service-manual",
			dbTrigger:      "industrial-work-order-created",
			timedTrigger:   "daily-industrial-work-order-review",
			webhookTrigger: "industrial-work-order-updates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moduleRoot := testsupport.ModuleRoot(t)
			eslint := filepath.Join(moduleRoot, "node_modules", "eslint", "bin", "eslint.js")
			if _, err := os.Stat(eslint); err != nil {
				t.Fatalf("ESLint is not installed at %s; run pnpm install at the repository root: %v", eslint, err)
			}
			if _, err := exec.LookPath("node"); err != nil {
				t.Fatalf("node is required to run ESLint: %v", err)
			}

			tempParent := filepath.Join(moduleRoot, "tmp")
			if err := os.MkdirAll(tempParent, fsx.DirPerm); err != nil {
				t.Fatalf("create temporary workspace parent: %v", err)
			}
			tempRoot, err := os.MkdirTemp(tempParent, "generated-lint-")
			if err != nil {
				t.Fatalf("create temporary workspace: %v", err)
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(tempRoot); err != nil {
					t.Errorf("remove temporary workspace: %v", err)
				}
				_ = os.Remove(tempParent)
			})

			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("get current directory: %v", err)
			}
			if err := os.Chdir(tempRoot); err != nil {
				t.Fatalf("change to temporary workspace parent: %v", err)
			}
			t.Cleanup(func() {
				if err := os.Chdir(oldDir); err != nil {
					t.Errorf("restore current directory: %v", err)
				}
			})

			invokeGeneratedWorkspaceCommand(t, "init", tt.projectName, "--tenant", tt.projectName)
			workspaceRoot := filepath.Join(tempRoot, tt.projectName)
			if err := os.Chdir(workspaceRoot); err != nil {
				t.Fatalf("change to generated workspace: %v", err)
			}

			invokeGeneratedWorkspaceCommand(t, "new", "app", tt.appID, "Field Service Management")
			invokeGeneratedWorkspaceCommand(t, "new", "action", tt.appID, tt.action, "Escalate Work Order", "--lang", "ts", "--scope", "acme")
			invokeGeneratedWorkspaceCommand(t, "new", "action", tt.appID, tt.rustAction, "Dispatch Technician", "--lang", "rust")
			invokeGeneratedWorkspaceCommand(t, "new", "action", tt.appID, tt.bothAction, "Handle Escalation", "--lang", "ts", "--scope", "acme", "--env", "both")
			invokeGeneratedWorkspaceCommand(t, "new", "agent", tt.appID, tt.agent, "Service Support")
			invokeGeneratedWorkspaceCommand(t, "new", "task-type", tt.appID, tt.taskType, "Service Requests", "--no-agent")
			invokeGeneratedWorkspaceCommand(t, "new", "task-type", tt.appID, tt.agentTaskType, "Triage Requests")
			invokeGeneratedWorkspaceCommand(t, "new", "skill", tt.appID, tt.skill, "Work Order Escalation", "--task-type", tt.taskType)
			invokeGeneratedWorkspaceCommand(t, "new", "knowledge", tt.appID, tt.knowledge, "Service Reference", "--task-type", tt.taskType)
			invokeGeneratedWorkspaceCommand(t, "new", "tool", tt.appID, tt.tool, "Look Up Work Order", "--scope", "acme", "--task-type", tt.taskType)
			invokeGeneratedWorkspaceCommand(t, "new", "tool", tt.appID, tt.rustTool, "Summarize Maintenance History", "--lang", "rust")
			invokeGeneratedWorkspaceCommand(t, "new", "trigger:db", tt.appID, tt.dbTrigger, "Work Order Created", "--action", tt.action, "--table", tt.behavior)
			invokeGeneratedWorkspaceCommand(t, "new", "trigger:timed", tt.appID, tt.timedTrigger, "Daily Work Order Review", "--action", tt.action, "--frequency", "daily", "--time", "09:00:00")
			invokeGeneratedWorkspaceCommand(t, "new", "trigger:webhook", tt.appID, tt.webhookTrigger, "Work Order Update", "--action", tt.action, "--method", "post", "--public")
			invokeGeneratedWorkspaceCommand(t, "new", "behavior", tt.appID, tt.behavior)
			invokeGeneratedWorkspaceCommand(t, "new", "space", tt.appID, tt.space, "Field Service Dashboard")

			// One run, read as JSON: the exit code says whether anything was found
			// and the report says which files were read. Only stdout is decoded,
			// so a notice on stderr cannot break the reading.
			command := exec.Command("node", eslint, ".", "--max-warnings", "0", "--format", "json")
			command.Dir = workspaceRoot
			// The rule set turns three rules down to warnings, and prints a notice
			// on stdout, when it believes it runs inside an editor. CI=true tells
			// it that it does not, so a run from an editor's terminal judges
			// exactly what a run anywhere else does and stdout holds only the
			// report.
			command.Env = append(os.Environ(), "CI=true")
			var stderr bytes.Buffer
			command.Stderr = &stderr
			report, runErr := command.Output()

			var lintFiles []struct {
				FilePath string `json:"filePath"`
				Messages []struct {
					RuleID  string `json:"ruleId"`
					Line    int    `json:"line"`
					Column  int    `json:"column"`
					Message string `json:"message"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(report, &lintFiles); err != nil {
				t.Fatalf("the linter produced no report (%v): %v\n%s\n%s", runErr, err, report, stderr.String())
			}

			// The file kinds the shipped setup is meant to read. Without this
			// list, a change that makes the linter skip one kind (a formatter
			// switched off, a wider ignore) would leave the test passing on files
			// nothing looked at.
			requiredEndings := []string{".ts", ".tsx", ".js", ".json", ".md", ".toml", ".css", ".html", ".yaml"}
			seenEndings := make(map[string]bool, len(requiredEndings))
			for _, file := range lintFiles {
				seenEndings[filepath.Ext(file.FilePath)] = true
				for _, message := range file.Messages {
					t.Errorf("%s:%d:%d %s (%s)", file.FilePath, message.Line, message.Column, message.Message, message.RuleID)
				}
			}
			for _, ending := range requiredEndings {
				if !seenEndings[ending] {
					t.Errorf("the linter read no %s file", ending)
				}
			}
			if runErr != nil && !t.Failed() {
				t.Errorf("the linter failed without naming a finding: %v\n%s", runErr, stderr.String())
			}
		})
	}
}

// resetCommandTree puts every command back as a fresh run of the tool finds
// it. The commands are package-level values, so what one test leaves on them
// is still there for the next: a flag it set, or an output writer it gave a
// command and has since closed.
//
// Only flags that were set are touched. A flag that holds a list cannot be
// put back through Set, which appends to the list rather than replacing it,
// so a list is emptied through its own interface. A command whose writers
// are cleared writes where its parent does.
func resetCommandTree(t *testing.T) {
	t.Helper()
	var reset func(*cobra.Command)
	reset = func(command *cobra.Command) {
		putBack := func(flag *pflag.Flag) {
			if !flag.Changed {
				return
			}
			if list, ok := flag.Value.(pflag.SliceValue); ok {
				if err := list.Replace(nil); err != nil {
					t.Errorf("reset %s flag %s: %v", command.CommandPath(), flag.Name, err)
				}
			} else if err := flag.Value.Set(flag.DefValue); err != nil {
				t.Errorf("reset %s flag %s: %v", command.CommandPath(), flag.Name, err)
			}
			flag.Changed = false
		}
		// A command's own flags, and the ones it hands down to its children,
		// which sit in a second set until the command has run once.
		command.Flags().VisitAll(putBack)
		command.PersistentFlags().VisitAll(putBack)
		command.SetOut(nil)
		command.SetErr(nil)
		for _, child := range command.Commands() {
			reset(child)
		}
	}
	reset(RootCmd)
}

// The check above lints with the packages installed for this module, while a
// new workspace installs the versions its own package.json names. If the two
// lists drift apart the check passes on a linter no workspace gets, so the four
// versions are held equal here.
func TestGeneratedLintDependencyVersionsMatch(t *testing.T) {
	moduleRoot := testsupport.ModuleRoot(t)
	cliManifest, err := readLintManifest(filepath.Join(moduleRoot, "package.json"))
	if err != nil {
		t.Fatalf("read CLI package.json: %v", err)
	}
	templateBytes, err := scaffold.TemplatesFS.ReadFile("templates/package.json")
	if err != nil {
		t.Fatalf("read template package.json: %v", err)
	}
	templateManifest, err := decodeLintManifest(templateBytes)
	if err != nil {
		t.Fatalf("decode template package.json: %v", err)
	}

	for _, dependency := range []string{"@antfu/eslint-config", "eslint", "eslint-plugin-format", "typescript"} {
		t.Run(dependency, func(t *testing.T) {
			cliVersion := cliManifest.DevDependencies[dependency]
			templateVersion := templateManifest.DevDependencies[dependency]
			if cliVersion == "" || templateVersion == "" || cliVersion != templateVersion {
				t.Errorf("CLI version %q and template version %q differ or are missing", cliVersion, templateVersion)
			}
		})
	}
}

func TestGeneratedPinnedDependencyVersionsMatch(t *testing.T) {
	contents, err := scaffold.TemplatesFS.ReadFile("templates/package.json")
	if err != nil {
		t.Fatalf("read workspace template package.json: %v", err)
	}
	manifest, err := decodeLintManifest(contents)
	if err != nil {
		t.Fatalf("decode workspace template package.json: %v", err)
	}
	for _, dependency := range []struct {
		name, version string
	}{
		{"ts-json-schema-generator", build.TSSchemaGeneratorVersion},
		{"ts-morph", build.TSMorphVersion},
		{"vitest", build.VitestVersion},
	} {
		t.Run(dependency.name, func(t *testing.T) {
			if got := manifest.DevDependencies[dependency.name]; got != dependency.version {
				t.Errorf("template version = %q, want pinned version %q", got, dependency.version)
			}
		})
	}
}

type lintManifest struct {
	DevDependencies map[string]string `json:"devDependencies"`
}

func readLintManifest(path string) (lintManifest, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return lintManifest{}, err
	}
	return decodeLintManifest(contents)
}

func decodeLintManifest(contents []byte) (lintManifest, error) {
	var manifest lintManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return lintManifest{}, err
	}
	return manifest, nil
}

// invokeGeneratedWorkspaceCommand runs one command as a fresh run of the tool
// would: with nothing left on it by an earlier command, in this test or in
// another. Without that, a task type created after one made with --no-agent
// would silently be made without its agent too.
func invokeGeneratedWorkspaceCommand(t *testing.T, args ...string) {
	t.Helper()
	resetCommandTree(t)
	stdout, stderr, err := invokeCmd(args...)
	if err != nil {
		t.Fatalf("simple %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout, stderr)
	}
}
