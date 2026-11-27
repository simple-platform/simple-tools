package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
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

// docsUpdateFS adds the two link-aware reads the update needs to keep every
// write and removal inside the repository.
type docsUpdateFS interface {
	fsx.FileSystem
	EvalSymlinks(name string) (string, error)
	Lstat(name string) (fs.FileInfo, error)
}

type docsUpdateDeps struct {
	fsys       docsUpdateFS
	templates  fsx.TemplateFS
	cwd        string
	cliVersion string
}

type docsUpdateResult struct {
	Updated         []string `json:"updated"`
	Removed         []string `json:"removed"`
	RemovalFailures []string `json:"removalFailures,omitempty"`
	UpToDate        bool     `json:"upToDate"`
}

func runDocsUpdate(out io.Writer, deps docsUpdateDeps, jsonMode bool) error {
	root, found := fsx.ResolveUpward(deps.fsys, deps.cwd, "simple.scl")
	if !found {
		return fmt.Errorf("run simple docs update inside a repository made with simple init")
	}
	root = filepath.Dir(root)
	realRoot, err := deps.fsys.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("failed to find the repository's real location, so simple docs update changed nothing: %w", err)
	}

	files, err := scaffold.DocumentationFiles(deps.templates)
	if err != nil {
		return err
	}
	fingerprint := scaffold.FingerprintDocumentationFiles(files)

	stampPath := filepath.Join(root, filepath.FromSlash(scaffold.DocumentationStampPath))
	if err := ensureDocumentationPath(deps.fsys, realRoot, root, scaffold.DocumentationStampPath); err != nil {
		return err
	}
	oldStamp, err := deps.fsys.ReadFile(stampPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read documentation stamp: %w", err)
	}
	var previous scaffold.DocumentationStamp
	stampReadable := err == nil && json.Unmarshal(oldStamp, &previous) == nil
	if !stampReadable {
		previous = scaffold.DocumentationStamp{}
	}
	if documentationStampIsNewer(previous.CLIVersion, deps.cliVersion) {
		return fmt.Errorf("this repository's docs were written by simple %s, which is newer than this one (%s). Run simple version update, then run simple docs update again", previous.CLIVersion, deps.cliVersion)
	}

	shipped := make(map[string]bool, len(files))
	shippedPaths := make([]string, len(files))
	for i, file := range files {
		shipped[file.Path] = true
		shippedPaths[i] = file.Path
	}
	var removable []string
	for _, oldPath := range previous.Files {
		if oldPath != scaffold.DocumentationStampPath && managedDocumentationPath(oldPath) && !shipped[oldPath] {
			removable = append(removable, oldPath)
		}
	}
	sort.Strings(removable)
	// Check every path before changing any, so a refusal leaves the repository as it was.
	for _, rel := range slices.Concat(removable, shippedPaths) {
		if err := ensureDocumentationPath(deps.fsys, realRoot, root, rel); err != nil {
			return err
		}
	}

	result := docsUpdateResult{Updated: []string{}, Removed: []string{}, RemovalFailures: []string{}}
	for _, oldPath := range removable {
		destination := filepath.Join(root, filepath.FromSlash(oldPath))
		info, err := deps.fsys.Lstat(destination)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			result.RemovalFailures = append(result.RemovalFailures, oldPath)
			continue
		}
		// A link is never removed, nor a folder: only a regular file the tool once shipped.
		if !info.Mode().IsRegular() {
			continue
		}
		if err := deps.fsys.Remove(destination); err != nil {
			result.RemovalFailures = append(result.RemovalFailures, oldPath)
			continue
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

	stamp, err := scaffold.MarshalDocumentationStamp(fingerprint, deps.cliVersion, files)
	if err != nil {
		return err
	}
	if !stampReadable || previous.Fingerprint != fingerprint || !slices.Equal(previous.Files, shippedPaths) {
		if err := deps.fsys.MkdirAll(filepath.Dir(stampPath), fsx.DirPerm); err != nil {
			return fmt.Errorf("failed to create documentation stamp directory: %w", err)
		}
		if err := deps.fsys.WriteFile(stampPath, stamp, fsx.FilePerm); err != nil {
			return fmt.Errorf("failed to write documentation stamp: %w", err)
		}
		result.Updated = append(result.Updated, scaffold.DocumentationStampPath)
	}

	result.UpToDate = len(result.Updated) == 0 && len(result.Removed) == 0 && len(result.RemovalFailures) == 0
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
	for _, name := range result.RemovalFailures {
		_, _ = fmt.Fprintf(out, "Could not remove %s\n", name)
	}
	_, _ = fmt.Fprintln(out, docsUpdateSummary(len(result.Updated), len(result.Removed)))
	for _, name := range result.Updated {
		if name == ".simple/context/.cli/AGENTS.template.md" || name == ".simple/context/.cli/cursorrules.template" {
			_, _ = fmt.Fprintln(out, "AGENTS.md and .cursorrules are your team's own files and were not changed. Current templates are in .simple/context/.cli/AGENTS.template.md and .simple/context/.cli/cursorrules.template.")
			break
		}
	}
	return nil
}

func documentationStampIsNewer(stampVersion, runningVersion string) bool {
	stamp, stampIsRelease := selfupdate.ParseVersion(stampVersion)
	running, runningIsRelease := selfupdate.ParseVersion(runningVersion)
	return stampIsRelease && runningIsRelease && stamp.NewerThan(running)
}

// ensureDocumentationPath refuses a guidance path whose real location is not
// inside the repository, so a link can never lead a write or a removal
// elsewhere. A path that does not exist yet is judged by the folder it would be
// created in. A link that leads nowhere is refused, since writing to it would
// create its target wherever it points.
func ensureDocumentationPath(fsys docsUpdateFS, realRoot, root, rel string) error {
	for candidate := filepath.Join(root, filepath.FromSlash(rel)); ; candidate = filepath.Dir(candidate) {
		resolved, err := fsys.EvalSymlinks(candidate)
		if err == nil {
			if pathIsWithinRoot(realRoot, resolved) {
				return nil
			}
			break
		}
		_, lstatErr := fsys.Lstat(candidate)
		if !os.IsNotExist(err) || !os.IsNotExist(lstatErr) || filepath.Dir(candidate) == candidate {
			break
		}
	}
	return fmt.Errorf("%s leads outside this repository or to nothing, so simple docs update changed nothing; replace the link with a folder or file inside the repository", rel)
}

func pathIsWithinRoot(root, name string) bool {
	relative, err := filepath.Rel(root, name)
	return err == nil && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func docsUpdateSummary(updated, removed int) string {
	return fmt.Sprintf("Summary: updated %s, removed %s.",
		pluralFiles(updated),
		pluralFiles(removed),
	)
}

func pluralFiles(count int) string {
	if count == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", count)
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
	stamp, err := fsys.ReadFile(filepath.Join(filepath.Dir(rootMarker), filepath.FromSlash(scaffold.DocumentationStampPath)))
	var recorded scaffold.DocumentationStamp
	if err == nil && json.Unmarshal(stamp, &recorded) == nil {
		if documentationStampIsNewer(recorded.CLIVersion, version) || recorded.Fingerprint == fingerprint {
			return
		}
	}
	_, _ = fmt.Fprint(out, docsUpdateNotice)
}
