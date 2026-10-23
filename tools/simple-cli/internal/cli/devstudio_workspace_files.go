package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"
)

// devStudioWorkspaceFiles are the root files every instance workspace needs,
// taken from the CLI's own scaffold: devbox.json and package.json pin the
// node and pnpm the agent's commands run with.
var devStudioWorkspaceFiles = []struct{ template, name string }{
	{"templates/devbox.json", "devbox.json"},
	{"templates/package.json", "package.json"},
}

// ensureDevStudioWorkspaceFiles gives a workspace made by an older CLI the
// root files a new one is scaffolded with. It adds a file only when it is
// missing, and only the .gitignore lines that are missing, so nothing a team
// wrote is changed.
func ensureDevStudioWorkspaceFiles(workspace *devStudioWorkspace) error {
	for _, file := range devStudioWorkspaceFiles {
		path, err := devStudioPathInside(workspace.instanceRoot, file.name)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to inspect %s: %w", file.name, err)
		}
		contents, err := scaffold.TemplatesFS.ReadFile(file.template)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, contents, fsx.FilePerm); err != nil {
			return fmt.Errorf("failed to write %s: %w", file.name, err)
		}
	}
	return ensureDevStudioGitignore(workspace.instanceRoot)
}

func ensureDevStudioGitignore(instanceRoot string) error {
	template, err := scaffold.TemplatesFS.ReadFile("templates/gitignore")
	if err != nil {
		return err
	}
	path, err := devStudioPathInside(instanceRoot, ".gitignore")
	if err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(path, template, fsx.FilePerm)
	}
	if err != nil {
		return fmt.Errorf("failed to read .gitignore: %w", err)
	}
	present := make(map[string]bool)
	for _, line := range strings.Split(string(current), "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, line := range strings.Split(string(template), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || present[line] {
			continue
		}
		missing = append(missing, line)
	}
	if len(missing) == 0 {
		return nil
	}
	addition := "\n# Added by DevStudio\n" + strings.Join(missing, "\n") + "\n"
	if len(current) > 0 && !bytes.HasSuffix(current, []byte("\n")) {
		addition = "\n" + addition
	}
	return os.WriteFile(path, append(current, addition...), fsx.FilePerm)
}
