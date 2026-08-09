package build

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"simple-cli/internal/fsx"
)

// A BUILD DESCRIBES ITS ACTIONS IN PARALLEL, AND THEY MUST NOT SHARE A FILE.
//
// The generator is a Node script that has to run from under the workspace root
// for ESM to resolve its packages, so it is written there before each run. It was
// written to ONE fixed path, and removed from it when the run finished. Every
// concurrent extraction in a build wrote the same bytes there, so nothing was
// ever corrupted — but one action's cleanup deleted the script another action's
// node process had not finished starting with, and that action failed to build
// with `Cannot find module` naming a path that appears nowhere in its source.
//
// Measured on the code this replaces: 1 failure in 48 actions, in each of two
// runs, at 16-way concurrency. That is a build that fails on which actions
// happened to finish first, which is the shape of failure nobody reproduces from
// a report.
//
// The concurrency here is what made it appear rather than a number chosen for
// speed: node's startup under contention is what widens the window between one
// invocation writing the script and another removing it.
func TestConcurrentExtractionsDoNotShareAScript(t *testing.T) {
	requireGenerator(t)

	root := t.TempDir()

	const actions = 48
	const concurrency = 16

	dirs := make([]string, actions)

	for index := range dirs {
		dir := filepath.Join(root, fmt.Sprintf("action-%02d", index))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create the action directory: %v", err)
		}

		source := fmt.Sprintf("/**\n * Action %d.\n */\nexport interface Payload { name: string }\n", index)
		if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte(source), 0644); err != nil {
			t.Fatalf("failed to write the action source: %v", err)
		}

		dirs[index] = dir
	}

	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup

	var mu sync.Mutex

	var failures []string

	for _, dir := range dirs {
		wg.Add(1)

		go func(dir string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			if err := describeActionFromSource(fsx.OSFileSystem{}, dir); err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(dir), err))
				mu.Unlock()
			}
		}(dir)
	}

	wg.Wait()

	if len(failures) > 0 {
		t.Fatalf("%d of %d concurrent extractions failed:\n%s", len(failures), actions, failures)
	}
}
