package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"simple-cli/internal/config"
	"simple-cli/internal/fsx"
	"simple-cli/internal/ui"
)

// DevStudio's playbook and skills are knowledge records on the instance,
// linked to the devstudio-session task type, so how the agent works changes
// with a record edit rather than a CLI release. Each time a browser connects,
// the bridge reads them through the hidden command below and hands them to
// Codex: the skills as files where Codex finds skills, the playbook with its
// instructions.

const (
	devStudioTaskTypeName = "devstudio-session"
	// devStudioKnowledgeTimeout bounds the whole read, a first sign-in
	// included, so a slow instance delays a connect rather than stopping it.
	devStudioKnowledgeTimeout = 20 * time.Second
	devStudioSkillsRecordFile = "skills.json"
)

// The query reads every task type's links and keeps DevStudio's, as Ally's
// host does: one read, and no reliance on filtering through a relationship.
const devStudioKnowledgeQuery = `query GetDevStudioKnowledge {
  task_types: dev_simple_system__task_types(where: {name: {_eq: "` + devStudioTaskTypeName + `"}}, limit: 2) {
    id
    playbook_id
  }
  task_type_skills: dev_simple_system__task_type_skills(order_by: {position: asc, id: asc}) {
    task_type_id
    knowledge {
      id
      title
      body
    }
  }
}`

var (
	// A skill's name is its folder, so it is a plain lower-case name.
	devStudioSkillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	devStudioSkillSlugPattern = regexp.MustCompile("(?m)^Slug:\\s*`([^`]+)`\\s*$")
)

var devStudioKnowledgeCmd = &cobra.Command{
	Use:    "knowledge",
	Short:  "Print the instance's DevStudio playbook and skills as JSON",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runDevStudioKnowledgeCommand,
}

func init() {
	devStudioCmd.AddCommand(devStudioKnowledgeCmd)
}

type devStudioKnowledge struct {
	Playbook string           `json:"playbook"`
	Skills   []devStudioSkill `json:"skills"`
}

type devStudioSkill struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type devStudioKnowledgeDeps struct {
	devops   devopsDeps
	client   *http.Client
	graphURL func(*config.Environment) string
}

func defaultDevStudioKnowledgeDeps() devStudioKnowledgeDeps {
	return devStudioKnowledgeDeps{
		devops: defaultDevopsDeps(),
		client: &http.Client{Timeout: devStudioKnowledgeTimeout},
		// The platform's GraphQL service, as the web app reaches it.
		graphURL: func(env *config.Environment) string { return "https://graph." + env.Endpoint },
	}
}

func runDevStudioKnowledgeCommand(cmd *cobra.Command, _ []string) error {
	knowledge, err := readDevStudioKnowledge(cmd.Context(), defaultDevStudioKnowledgeDeps())
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(knowledge)
}

// readDevStudioKnowledge signs in to the devstudio environment of the
// simple.scl in the working directory and reads its DevStudio playbook and
// skills. A session the instance refuses gets one fresh sign-in, as a deploy's
// does: the platform can revoke a session before it expires.
func readDevStudioKnowledge(ctx context.Context, deps devStudioKnowledgeDeps) (devStudioKnowledge, error) {
	target, err := loadDevopsTarget(deps.devops, "devstudio", ui.NopReporter{}, nil)
	if err != nil {
		return devStudioKnowledge{}, err
	}
	if err := target.authenticate(ctx); err != nil {
		return devStudioKnowledge{}, err
	}
	url := deps.graphURL(target.env)
	status, body, err := postDevStudioKnowledgeQuery(ctx, deps.client, url, target.jwt)
	if err == nil && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
		if err := target.auth.ClearCache(target.tenantEnvKey); err != nil {
			return devStudioKnowledge{}, fmt.Errorf("failed to clear the refused session: %w", err)
		}
		if err := target.authenticate(ctx); err != nil {
			return devStudioKnowledge{}, err
		}
		status, body, err = postDevStudioKnowledgeQuery(ctx, deps.client, url, target.jwt)
	}
	if err != nil {
		return devStudioKnowledge{}, err
	}
	return devStudioKnowledgeFrom(status, body)
}

func postDevStudioKnowledgeQuery(ctx context.Context, client *http.Client, url, jwt string) (int, []byte, error) {
	payload, err := json.Marshal(map[string]string{"query": devStudioKnowledgeQuery})
	if err != nil {
		return 0, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+jwt)
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to reach the instance's GraphQL service: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read the instance's answer: %w", err)
	}
	return response.StatusCode, body, nil
}

// devStudioKnowledgeFrom picks DevStudio's links out of the instance's answer:
// the linked record that is the task type's playbook, and the others, in the
// order the links state, as its skills.
func devStudioKnowledgeFrom(status int, body []byte) (devStudioKnowledge, error) {
	var answer struct {
		Data struct {
			TaskTypes []struct {
				ID         string `json:"id"`
				PlaybookID string `json:"playbook_id"`
			} `json:"task_types"`
			Links []struct {
				TaskTypeID string `json:"task_type_id"`
				Knowledge  *struct {
					ID    string `json:"id"`
					Title string `json:"title"`
					Body  string `json:"body"`
				} `json:"knowledge"`
			} `json:"task_type_skills"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	// GraphQL errors arrive with a 200 or a 400, so the body is read first.
	if err := json.Unmarshal(body, &answer); err != nil {
		return devStudioKnowledge{}, fmt.Errorf("the instance answered HTTP %d with something other than GraphQL", status)
	}
	if len(answer.Errors) > 0 {
		messages := make([]string, 0, len(answer.Errors))
		for _, item := range answer.Errors {
			messages = append(messages, item.Message)
		}
		return devStudioKnowledge{}, fmt.Errorf("the instance refused the read: %s", strings.Join(messages, "; "))
	}
	if status != http.StatusOK {
		return devStudioKnowledge{}, fmt.Errorf("the instance answered HTTP %d", status)
	}
	if len(answer.Data.TaskTypes) != 1 {
		return devStudioKnowledge{}, fmt.Errorf("the instance has %d task types named %s, not one", len(answer.Data.TaskTypes), devStudioTaskTypeName)
	}
	taskType := answer.Data.TaskTypes[0]
	knowledge := devStudioKnowledge{Skills: []devStudioSkill{}}
	for _, link := range answer.Data.Links {
		if link.TaskTypeID != taskType.ID || link.Knowledge == nil {
			continue
		}
		if link.Knowledge.ID == taskType.PlaybookID {
			knowledge.Playbook = link.Knowledge.Body
			continue
		}
		knowledge.Skills = append(knowledge.Skills, devStudioSkill{Title: link.Knowledge.Title, Body: link.Knowledge.Body})
	}
	return knowledge, nil
}

// devStudioKnowledgeFetcher reads an instance's DevStudio playbook and skills.
type devStudioKnowledgeFetcher func(workspace *devStudioWorkspace, deployKey string) (devStudioKnowledge, error)

// fetchDevStudioKnowledge runs this CLI's hidden knowledge command in the
// workspace, with the environment the agent's commands get. It therefore signs
// in with the workspace's own key pair and session, as a deploy from Codex
// would, and never sets up a key of its own.
func fetchDevStudioKnowledge(workspace *devStudioWorkspace, deployKey string) (devStudioKnowledge, error) {
	executable, err := os.Executable()
	if err != nil {
		return devStudioKnowledge{}, fmt.Errorf("failed to find the Simple CLI: %w", err)
	}
	environment, err := devStudioCodexEnvironment(workspace.instanceRoot, workspace.instanceID, deployKey)
	if err != nil {
		return devStudioKnowledge{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), devStudioKnowledgeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "--json", "devstudio", "knowledge")
	cmd.Dir = workspace.instanceRoot
	cmd.Env = environment
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return devStudioKnowledge{}, fmt.Errorf("%w: %s", err, devStudioOneLine(stderr.String()))
	}
	var knowledge devStudioKnowledge
	if err := json.Unmarshal(output, &knowledge); err != nil {
		return devStudioKnowledge{}, fmt.Errorf("the knowledge command printed something other than JSON: %w", err)
	}
	return knowledge, nil
}

// devStudioOneLine is the command's error output on one line: with --json the
// CLI prints its error as an indented JSON document.
func devStudioOneLine(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	if runes := []rune(line); len(runes) > 500 {
		return string(runes[:500])
	}
	return line
}

// deliverKnowledge reads the instance's DevStudio playbook and skills, writes
// the skills for Codex and returns the playbook. When the instance cannot be
// read it returns "": the session then runs on the built-in instructions and
// the skills delivered last time, so a connect never waits on it to work.
func (bridge *devStudioBridge) deliverKnowledge(workspace *devStudioWorkspace, deployKey string) string {
	if bridge.knowledgeFetcher == nil {
		return ""
	}
	knowledge, err := bridge.knowledgeFetcher(workspace, deployKey)
	if err != nil {
		log.Printf("[DevStudio Bridge] The DevStudio playbook and skills could not be read; using the built-in instructions: %s", devStudioLogValue(err))
		return ""
	}
	if err := deliverDevStudioSkills(workspace, knowledge.Skills); err != nil {
		log.Printf("[DevStudio Bridge] The DevStudio skills could not be written: %s", devStudioLogValue(err))
	}
	return knowledge.Playbook
}

// deliverDevStudioSkills writes each skill to .agents/skills/<name>/SKILL.md,
// where Codex finds a workspace's skills, and removes the ones an earlier
// delivery wrote that the instance no longer links. What was delivered is
// recorded beside the conversation, outside the workspace, so the agent can
// neither hide a delivered skill from the clean-up nor point it at its own.
func deliverDevStudioSkills(workspace *devStudioWorkspace, skills []devStudioSkill) error {
	delivered := make([]string, 0, len(skills))
	for _, skill := range skills {
		name := devStudioSkillName(skill)
		if name == "" {
			log.Printf("[DevStudio Bridge] Skipping the skill %s: it names no usable Slug", devStudioLogValue(skill.Title))
			continue
		}
		path, err := devStudioPathInside(workspace.instanceRoot, ".agents", "skills", name, "SKILL.md")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), fsx.DirPerm); err != nil {
			return fmt.Errorf("failed to create the folder of skill %s: %w", name, err)
		}
		if err := os.WriteFile(path, devStudioSkillFile(name, skill), fsx.FilePerm); err != nil {
			return fmt.Errorf("failed to write skill %s: %w", name, err)
		}
		delivered = append(delivered, name)
	}

	recordPath, err := devStudioSkillsRecordPath(workspace.instanceID)
	if err != nil {
		return err
	}
	var previous []string
	if contents, err := os.ReadFile(recordPath); err == nil {
		_ = json.Unmarshal(contents, &previous)
	}
	for _, name := range previous {
		if slices.Contains(delivered, name) || !devStudioSkillNamePattern.MatchString(name) {
			continue
		}
		path, err := devStudioPathInside(workspace.instanceRoot, ".agents", "skills", name)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("failed to remove skill %s: %w", name, err)
		}
	}
	contents, err := json.Marshal(delivered)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(recordPath), fsx.DirPerm); err != nil {
		return fmt.Errorf("failed to record the delivered skills: %w", err)
	}
	return os.WriteFile(recordPath, contents, fsx.FilePerm)
}

func devStudioSkillsRecordPath(instanceID string) (string, error) {
	threadPath, err := devStudioThreadPath(instanceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(threadPath), devStudioSkillsRecordFile), nil
}

// devStudioSkillName is the skill's Slug line, the name every skill body
// carries, or "" when it has none that can name a folder.
func devStudioSkillName(skill devStudioSkill) string {
	matches := devStudioSkillSlugPattern.FindStringSubmatch(skill.Body)
	if matches == nil || len(matches[1]) > 64 || !devStudioSkillNamePattern.MatchString(matches[1]) {
		return ""
	}
	return matches[1]
}

// devStudioSkillFile is a skill as Codex reads one: its name and a
// description it decides from when to load the skill, then the body as it is.
func devStudioSkillFile(name string, skill devStudioSkill) []byte {
	// A JSON string is a valid YAML double-quoted string.
	description, _ := json.Marshal(devStudioSkillDescription(skill))
	return []byte("---\nname: " + name + "\ndescription: " + string(description) + "\n---\n\n" + skill.Body)
}

// devStudioSkillDescription is the first paragraph under the body's Purpose
// heading, the same text the platform's skill index shows, or the title when
// the body has none.
func devStudioSkillDescription(skill devStudioSkill) string {
	var paragraph []string
	inPurpose := false
	for _, line := range strings.Split(skill.Body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !inPurpose:
			inPurpose = trimmed == "## Purpose"
		case trimmed == "" && len(paragraph) > 0:
			return devStudioShortDescription(strings.Join(paragraph, " "))
		case strings.HasPrefix(trimmed, "#"):
			return devStudioShortDescription(strings.Join(paragraph, " "))
		case trimmed != "":
			paragraph = append(paragraph, trimmed)
		}
	}
	if len(paragraph) > 0 {
		return devStudioShortDescription(strings.Join(paragraph, " "))
	}
	return skill.Title
}

func devStudioShortDescription(text string) string {
	if runes := []rune(text); len(runes) > 1024 {
		return string(runes[:1024])
	}
	return text
}
