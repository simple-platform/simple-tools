package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"simple-cli/internal/fsx"
	"simple-cli/internal/scaffold"
	"simple-cli/internal/selfupdate"
)

const docsUpdateNotice = "\nThis repository's docs for coding assistants are older than this CLI's.\nRun `simple docs update` to refresh them.\n"

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Manage the guidance shipped for coding assistants",
	Args:  cobra.NoArgs,
}

var docsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the guidance shipped for coding assistants; never touch packages",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to find the current directory: %w", err)
		}
		deps := docsUpdateDeps{fsys: fsx.OSFileSystem{}, templates: scaffold.TemplatesFS, cwd: cwd, cliVersion: Version}
		return runDocsUpdate(cmd.OutOrStdout(), deps, jsonOutput)
	},
}

func init() {
	RootCmd.AddCommand(docsCmd)
	docsCmd.AddCommand(docsUpdateCmd)
}

type docsUpdateDeps struct {
	fsys       fsx.FileSystem
	templates  fsx.TemplateFS
	cwd        string
	cliVersion string
}

type docsUpdateResult struct {
	Updated  []string `json:"updated"`
	Removed  []string `json:"removed"`
	Differs  []string `json:"differs"`
	UpToDate bool     `json:"upToDate"`
}

func runDocsUpdate(out io.Writer, deps docsUpdateDeps, jsonMode bool) error {
	root, found := fsx.ResolveUpward(deps.fsys, deps.cwd, "simple.scl")
	if !found {
		return fmt.Errorf("run simple docs update inside a repository made with simple init")
	}
	root = filepath.Dir(root)

	files, err := scaffold.DocumentationFiles(deps.templates)
	if err != nil {
		return err
	}
	fingerprint := scaffold.FingerprintDocumentationFiles(files)

	stampPath := filepath.Join(root, ".simple", "docs.json")
	oldStamp, stampErr := deps.fsys.ReadFile(stampPath)
	if stampErr != nil && !os.IsNotExist(stampErr) {
		return fmt.Errorf("failed to read documentation stamp: %w", stampErr)
	}
	var previous scaffold.DocumentationStamp
	if stampErr == nil {
		_ = json.Unmarshal(oldStamp, &previous)
	}

	shipped := make(map[string]bool, len(files))
	for _, file := range files {
		shipped[file.Path] = true
	}
	result := docsUpdateResult{Updated: []string{}, Removed: []string{}, Differs: []string{}}
	oldPaths := append([]string(nil), previous.Files...)
	sort.Strings(oldPaths)
	for _, oldPath := range oldPaths {
		if !managedDocumentationPath(oldPath) || shipped[oldPath] {
			continue
		}
		destination := filepath.Join(root, filepath.FromSlash(oldPath))
		if _, err := deps.fsys.Stat(destination); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to check obsolete documentation %s: %w", oldPath, err)
		}
		if err := deps.fsys.Remove(destination); err != nil {
			return fmt.Errorf("failed to remove obsolete documentation %s: %w", oldPath, err)
		}
		result.Removed = append(result.Removed, oldPath)
	}

	for _, file := range files {
		destination := filepath.Join(root, filepath.FromSlash(file.Path))
		current, err := deps.fsys.ReadFile(destination)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to read documentation %s: %w", file.Path, err)
		}
		if err == nil && bytes.Equal(current, file.Content) {
			continue
		}
		if err := deps.fsys.MkdirAll(filepath.Dir(destination), fsx.DirPerm); err != nil {
			return fmt.Errorf("failed to create documentation directory for %s: %w", file.Path, err)
		}
		if err := deps.fsys.WriteFile(destination, file.Content, fsx.FilePerm); err != nil {
			return fmt.Errorf("failed to write documentation %s: %w", file.Path, err)
		}
		result.Updated = append(result.Updated, file.Path)
	}

	for _, rootFile := range []struct {
		path     string
		template string
	}{
		{path: "AGENTS.md", template: "templates/AGENTS.md"},
		{path: ".cursorrules", template: "templates/cursorrules"},
	} {
		expected, err := deps.templates.ReadFile(rootFile.template)
		if err != nil {
			return fmt.Errorf("failed to read root guidance template %s: %w", rootFile.template, err)
		}
		current, err := deps.fsys.ReadFile(filepath.Join(root, filepath.FromSlash(rootFile.path)))
		if err != nil {
			if os.IsNotExist(err) {
				result.Differs = append(result.Differs, rootFile.path)
				continue
			}
			return fmt.Errorf("failed to read root guidance %s: %w", rootFile.path, err)
		}
		if !bytes.Equal(current, expected) {
			result.Differs = append(result.Differs, rootFile.path)
		}
	}

	stamp, err := scaffold.MarshalDocumentationStamp(fingerprint, deps.cliVersion, files)
	if err != nil {
		return err
	}
	if stampErr != nil || !bytes.Equal(oldStamp, stamp) {
		if err := deps.fsys.MkdirAll(filepath.Dir(stampPath), fsx.DirPerm); err != nil {
			return fmt.Errorf("failed to create documentation stamp directory: %w", err)
		}
		if err := deps.fsys.WriteFile(stampPath, stamp, fsx.FilePerm); err != nil {
			return fmt.Errorf("failed to write documentation stamp: %w", err)
		}
		result.Updated = append(result.Updated, ".simple/docs.json")
	}

	result.UpToDate = len(result.Updated) == 0 && len(result.Removed) == 0 && len(result.Differs) == 0
	if jsonMode {
		return printJSONTo(out, result)
	}
	if result.UpToDate {
		_, _ = fmt.Fprintln(out, "Docs are already up to date.")
		return nil
	}
	for _, name := range result.Updated {
		_, _ = fmt.Fprintf(out, "Updated %s\n", name)
	}
	for _, name := range result.Removed {
		_, _ = fmt.Fprintf(out, "Removed %s\n", name)
	}
	for _, name := range result.Differs {
		_, _ = fmt.Fprintf(out, "%s differs from the current template. Compare it by hand; it was not changed.\n", name)
	}
	_, _ = fmt.Fprintln(out, docsUpdateSummary(len(result.Updated), len(result.Removed), len(result.Differs)))
	return nil
}

func docsUpdateSummary(updated, removed, differs int) string {
	return fmt.Sprintf("Summary: updated %s, removed %s, %s.",
		pluralFiles(updated),
		pluralFiles(removed),
		pluralRootFiles(differs),
	)
}

func pluralFiles(count int) string {
	if count == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", count)
}

func pluralRootFiles(count int) string {
	if count == 1 {
		return "1 root file differs"
	}
	return fmt.Sprintf("%d root files differ", count)
}

func managedDocumentationPath(name string) bool {
	if name == "" || strings.Contains(name, "\\") || path.Clean(name) != name || path.IsAbs(name) {
		return false
	}
	return strings.HasPrefix(name, ".agent/") || strings.HasPrefix(name, ".simple/context/")
}

func announceDocsUpdate(errOut io.Writer, commandPath string) {
	announceDocsUpdateTo(errOut, Version, jsonOutput, outputIsTerminal(errOut), os.Getenv("CI"), commandPath, fsx.OSFileSystem{}, os.Getwd, scaffold.DocumentationFingerprint)
}

func announceDocsUpdateTo(out io.Writer, version string, jsonMode, errIsTerminal bool, ciEnv, commandPath string, fsys fsx.FileSystem, getwd func() (string, error), fingerprintFor func() (string, error)) {
	_, released := selfupdate.ParseVersion(version)
	if !released || !updateNoticeWanted(jsonMode, errIsTerminal, ciEnv, commandPath) {
		return
	}
	names := strings.Fields(commandPath)
	if len(names) > 1 && (names[1] == "init" || names[1] == "docs") {
		return
	}
	cwd, err := getwd()
	if err != nil {
		return
	}
	rootMarker, found := fsx.ResolveUpward(fsys, cwd, "simple.scl")
	if !found {
		return
	}
	fingerprint, err := fingerprintFor()
	if err != nil {
		_, _ = fmt.Fprint(out, docsUpdateNotice)
		return
	}
	stamp, err := fsys.ReadFile(filepath.Join(filepath.Dir(rootMarker), ".simple", "docs.json"))
	var recorded scaffold.DocumentationStamp
	if err == nil && json.Unmarshal(stamp, &recorded) == nil && recorded.Fingerprint == fingerprint {
		return
	}
	_, _ = fmt.Fprint(out, docsUpdateNotice)
}
