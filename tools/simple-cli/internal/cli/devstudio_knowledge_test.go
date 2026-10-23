package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"simple-cli/internal/config"
)

// devStudioKnowledgeResponse is what the instance answers for the knowledge
// query: one task type of another kind of work, then DevStudio's, whose links
// carry its playbook and one skill.
const devStudioKnowledgeResponse = `{"data":{
  "task_types":[{"id":"TT1","playbook_id":"K1"}],
  "task_type_skills":[
    {"task_type_id":"TT9","knowledge":{"id":"K9","title":"Another Kind Of Work","body":"Slug: ` + "`skill-other`" + `"}},
    {"task_type_id":"TT1","knowledge":{"id":"K1","title":"DevStudio Playbook","body":"Follow the skills."}},
    {"task_type_id":"TT1","knowledge":{"id":"K2","title":"Sorting A Request","body":"## Purpose\n\nSort the request.\n\nSlug: ` + "`skill-devstudio-triage`" + `\n"}}
  ]}}`

func devStudioKnowledgeTestDeps(auth *fakeAuthenticator, url string) devStudioKnowledgeDeps {
	return devStudioKnowledgeDeps{
		devops: devopsDeps{
			ensureParser: func(func(string)) (string, error) { return "/fake/scl-parser", nil },
			loadConfig: func(string, func(string)) (*config.SimpleSCL, error) {
				return &config.SimpleSCL{Tenant: "acme", Environments: map[string]*config.Environment{
					"devstudio": {Name: "devstudio", Endpoint: "acme.simple.dev", APIKey: "si_key"},
				}}, nil
			},
			newAuthenticator: func() devopsAuthenticator { return auth },
		},
		client:   http.DefaultClient,
		graphURL: func(*config.Environment) string { return url },
	}
}

func TestDevStudioKnowledgeReadsThePlaybookAndSkillsOfDevStudioOnly(t *testing.T) {
	var authorization, query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		query = string(body)
		_, _ = io.WriteString(w, devStudioKnowledgeResponse)
	}))
	defer server.Close()

	knowledge, err := readDevStudioKnowledge(context.Background(), devStudioKnowledgeTestDeps(&fakeAuthenticator{}, server.URL))
	if err != nil {
		t.Fatalf("readDevStudioKnowledge: %v", err)
	}
	if authorization != "Bearer jwt-1" {
		t.Fatalf("Authorization = %q, want the session the workspace key signed in for", authorization)
	}
	if !strings.Contains(query, "devstudio-session") {
		t.Fatalf("the query does not name DevStudio's task type: %s", query)
	}
	if knowledge.Playbook != "Follow the skills." {
		t.Fatalf("playbook = %q", knowledge.Playbook)
	}
	if len(knowledge.Skills) != 1 || knowledge.Skills[0].Title != "Sorting A Request" {
		t.Fatalf("skills = %+v, want only DevStudio's skill, without its playbook", knowledge.Skills)
	}
}

func TestDevStudioKnowledgeSignsInOnceMoreWhenTheSessionIsRefused(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		first := len(seen) == 1
		mu.Unlock()
		if first {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, devStudioKnowledgeResponse)
	}))
	defer server.Close()
	auth := &fakeAuthenticator{}

	if _, err := readDevStudioKnowledge(context.Background(), devStudioKnowledgeTestDeps(auth, server.URL)); err != nil {
		t.Fatalf("readDevStudioKnowledge: %v", err)
	}
	if strings.Join(seen, ",") != "Bearer jwt-1,Bearer jwt-2" {
		t.Fatalf("sessions sent = %v, want the refused one, then a fresh one", seen)
	}
	if strings.Join(auth.cleared, ",") != "acme::devstudio" {
		t.Fatalf("cleared token caches = %v, want only the workspace's devstudio session", auth.cleared)
	}
}

func TestDevStudioKnowledgeReportsTheInstancesErrors(t *testing.T) {
	for name, response := range map[string]string{
		"query error":   `{"errors":[{"message":"field task_types not allowed"}]}`,
		"no task type":  `{"data":{"task_types":[],"task_type_skills":[]}}`,
		"two task type": `{"data":{"task_types":[{"id":"A"},{"id":"B"}],"task_type_skills":[]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, response)
			}))
			defer server.Close()
			if _, err := readDevStudioKnowledge(context.Background(), devStudioKnowledgeTestDeps(&fakeAuthenticator{}, server.URL)); err == nil {
				t.Fatal("readDevStudioKnowledge succeeded on an answer it cannot use")
			}
		})
	}
}

func devStudioTestSkill(slug, purpose string) devStudioSkill {
	return devStudioSkill{
		Title: "Skill " + slug,
		Body:  "# Skill\n\n## Purpose\n\n" + purpose + "\n\nSlug: `" + slug + "`\n\n## When To Use\n\nAlways.\n",
	}
}

func TestDevStudioSkillsAreWrittenWhereCodexFindsThem(t *testing.T) {
	workspace := devStudioTestWorkspace(t)
	skill := devStudioTestSkill("skill-devstudio-triage", "Sort a request: \"new\" or\nnot.")

	if err := deliverDevStudioSkills(workspace, []devStudioSkill{skill}); err != nil {
		t.Fatalf("deliverDevStudioSkills: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(workspace.instanceRoot, ".agents", "skills", "skill-devstudio-triage", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill file: %v", err)
	}
	want := "---\nname: skill-devstudio-triage\ndescription: \"Sort a request: \\\"new\\\" or not.\"\n---\n\n" + skill.Body
	if string(contents) != want {
		t.Fatalf("SKILL.md =\n%s\nwant\n%s", contents, want)
	}
}

func TestDevStudioSkillsNoLongerLinkedAreRemovedAndOthersKept(t *testing.T) {
	workspace := devStudioTestWorkspace(t)
	skills := filepath.Join(workspace.instanceRoot, ".agents", "skills")
	if err := os.MkdirAll(filepath.Join(skills, "team-own-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	first := []devStudioSkill{devStudioTestSkill("skill-a", "A."), devStudioTestSkill("skill-b", "B.")}
	if err := deliverDevStudioSkills(workspace, first); err != nil {
		t.Fatalf("first delivery: %v", err)
	}

	if err := deliverDevStudioSkills(workspace, []devStudioSkill{devStudioTestSkill("skill-a", "A.")}); err != nil {
		t.Fatalf("second delivery: %v", err)
	}
	for name, want := range map[string]bool{"skill-a": true, "skill-b": false, "team-own-skill": true} {
		_, err := os.Stat(filepath.Join(skills, name))
		if got := err == nil; got != want {
			t.Fatalf("%s present = %t, want %t", name, got, want)
		}
	}
}

func TestDevStudioSkillsWithoutAUsableNameAreSkipped(t *testing.T) {
	workspace := devStudioTestWorkspace(t)
	skills := []devStudioSkill{
		{Title: "No slug", Body: "## Purpose\n\nNothing names me.\n"},
		devStudioTestSkill("../escape", "Leaves the folder."),
		devStudioTestSkill("Skill_Upper", "Not a skill name."),
	}

	if err := deliverDevStudioSkills(workspace, skills); err != nil {
		t.Fatalf("deliverDevStudioSkills: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(workspace.instanceRoot, ".agents", "skills"))
	if len(entries) != 0 {
		t.Fatalf("written skills = %v, want none", entries)
	}
	if _, err := os.Stat(filepath.Join(workspace.instanceRoot, ".agents", "escape")); !os.IsNotExist(err) {
		t.Fatalf("a skill name led outside the skills folder: %v", err)
	}
}

func TestDevStudioThreadCarriesThePlaybook(t *testing.T) {
	workspace := &devStudioWorkspace{instanceID: "inst_playbook", instanceRoot: "/instances/inst_playbook"}
	adapter := newDevStudioCodexAdapter(nil, "inst_playbook", workspace, "", nil)
	builtIn := devStudioCodexInstructions("inst_playbook", "/instances/inst_playbook")

	if got := adapter.threadParams()["developerInstructions"]; got != builtIn {
		t.Fatalf("without a playbook, instructions = %v, want the built-in ones", got)
	}
	adapter.playbook = "Sort every request first."
	got, _ := adapter.threadParams()["developerInstructions"].(string)
	if !strings.HasPrefix(got, builtIn) || !strings.HasSuffix(got, "Sort every request first.") {
		t.Fatalf("with a playbook, instructions = %q", got)
	}
}

func TestDevStudioConnectHandsTheInstancesKnowledgeToCodex(t *testing.T) {
	baseDir := setupTestProductionRoot(t, "inst_codex")
	createDevStudioApp(t, baseDir, "inst_codex", "app-one")
	instanceRoot := filepath.Join(baseDir, "inst_codex")
	starter := newFakeDevStudioCodexStarter(nil)
	bridge := newDevStudioBridgeWithCodexStarter(starter)
	fetchedKeys := make(chan string, 1)
	bridge.knowledgeFetcher = func(_ *devStudioWorkspace, deployKey string) (devStudioKnowledge, error) {
		if starter.startCount() != 0 {
			t.Error("the knowledge was read after Codex started")
		}
		fetchedKeys <- deployKey
		return devStudioKnowledge{Playbook: "Sort every request first.", Skills: []devStudioSkill{devStudioTestSkill("skill-devstudio-triage", "Sort.")}}, nil
	}

	connectDevStudioSession(t, bridge, "inst_codex")

	if key := <-fetchedKeys; key != "test_deploy_key_secret" {
		t.Fatal("the knowledge was not read with the session's key")
	}
	threadStart, _ := starter.request("thread/start")
	if instructions, _ := threadStart.Params["developerInstructions"].(string); !strings.HasSuffix(instructions, "Sort every request first.") {
		t.Fatalf("thread/start instructions do not carry the playbook: %q", instructions)
	}
	for _, file := range []string{".agents/skills/skill-devstudio-triage/SKILL.md", "devbox.json", "package.json"} {
		if _, err := os.Stat(filepath.Join(instanceRoot, file)); err != nil {
			t.Fatalf("%s after connect: %v", file, err)
		}
	}
}
