package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"simple-cli/internal/fsx"
)

type dependencyLockState uint8

const (
	dependencyLockMissing dependencyLockState = iota
	dependencyLockUntracked
	dependencyLockUncommitted
	dependencyLockCommitted
	dependencyLockGitUnavailable
	dependencyLockNotRepository
)

type dependencyLockCheck struct {
	state dependencyLockState
	root  string
}

const devStudioWorkspaceMarker = ".simple/devstudio-workspace"
const devStudioWorkspaceMarkerValue = "managed by simple devstudio\n"

// npmFolders filters build targets to the folders whose dependencies npm owns.
func npmFolders(fsys fsx.FileSystem, actionDirs, spaceDirs []string) ([]string, error) {
	folders := make([]string, 0, len(actionDirs)+len(spaceDirs))
	seen := make(map[string]struct{}, len(actionDirs)+len(spaceDirs))
	for _, folder := range append(append([]string(nil), actionDirs...), spaceDirs...) {
		key := filepath.Clean(folder)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if _, err := fsys.Stat(filepath.Join(folder, "package.json")); err == nil {
			folders = append(folders, folder)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("failed to check for package.json in %s: %w", folder, err)
		}
	}
	return folders, nil
}

// checkDependencyLock reports the Git state of one folder's own lock file.
// Every Git path is relative to the folder passed with -C, never the caller's cwd.
func checkDependencyLock(folder string) (dependencyLockCheck, error) {
	lockPath := filepath.Join(folder, "package-lock.json")
	if _, err := os.Stat(lockPath); errors.Is(err, os.ErrNotExist) {
		return dependencyLockCheck{state: dependencyLockMissing}, nil
	} else if err != nil {
		return dependencyLockCheck{}, fmt.Errorf("failed to check %s: %w", lockPath, err)
	}

	gitPath := filepath.ToSlash(filepath.Join(".", "package-lock.json"))
	rootOutput, err := gitCommand(folder, "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		if isGitUnavailable(err) {
			return dependencyLockCheck{state: dependencyLockGitUnavailable}, nil
		}
		if isGitNotRepository(rootOutput) {
			return dependencyLockCheck{state: dependencyLockNotRepository}, nil
		}
		return dependencyLockCheck{}, fmt.Errorf("failed to find Git repository for %s: %w", folder, err)
	}
	root := strings.TrimSpace(string(rootOutput))
	if root == "" {
		return dependencyLockCheck{state: dependencyLockNotRepository}, nil
	}

	trackedOutput, err := gitCommand(folder, "ls-files", "--error-unmatch", "--", gitPath).CombinedOutput()
	if err != nil {
		if isGitNotRepository(trackedOutput) {
			return dependencyLockCheck{state: dependencyLockNotRepository}, nil
		}
		if isGitUnavailable(err) {
			return dependencyLockCheck{state: dependencyLockGitUnavailable}, nil
		}
		if strings.Contains(string(trackedOutput), "did not match any file") || len(trackedOutput) == 0 {
			return dependencyLockCheck{state: dependencyLockUntracked, root: root}, nil
		}
		return dependencyLockCheck{}, fmt.Errorf("failed to check whether %s is tracked: %w", lockPath, err)
	}

	statusOutput, err := gitCommand(folder, "status", "--porcelain", "--", gitPath).CombinedOutput()
	if err != nil {
		if isGitNotRepository(statusOutput) {
			return dependencyLockCheck{state: dependencyLockNotRepository}, nil
		}
		if isGitUnavailable(err) {
			return dependencyLockCheck{state: dependencyLockGitUnavailable}, nil
		}
		return dependencyLockCheck{}, fmt.Errorf("failed to check Git status for %s: %w", lockPath, err)
	}
	if strings.TrimSpace(string(statusOutput)) != "" {
		return dependencyLockCheck{state: dependencyLockUncommitted, root: root}, nil
	}
	return dependencyLockCheck{state: dependencyLockCommitted, root: root}, nil
}

func gitCommand(folder string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", folder}, args...)...)
	cmd.Env = make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "LC_ALL=") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	return cmd
}

func isGitUnavailable(err error) bool {
	var commandError *exec.Error
	return errors.As(err, &commandError)
}

func isGitNotRepository(output []byte) bool {
	return strings.Contains(strings.ToLower(string(output)), "not a git repository")
}

func isDevStudioWorkspace(folder string) bool {
	current, err := filepath.Abs(folder)
	if err != nil {
		return false
	}
	for {
		markerPath := filepath.Join(current, filepath.FromSlash(devStudioWorkspaceMarker))
		contents, err := os.ReadFile(markerPath)
		if err == nil && string(contents) == devStudioWorkspaceMarkerValue {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}
