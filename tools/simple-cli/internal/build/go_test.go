package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"simple-cli/internal/fsx"
)

// goAction lays out the smallest directory DetectActionLanguage calls Go.
func goAction(t *testing.T, dir string) string {
	t.Helper()

	actionDir := filepath.Join(dir, "sync-orders")
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionDir, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	return actionDir
}

// goBuildHarness swaps in the seams a Go action's build path reaches for, and
// records what it did with them.
type goBuildHarness struct {
	mu        sync.Mutex
	described []string
}

// withGoBuildHarness stands in for the description step and the SCL read, and
// hands back what the build asked of them.
//
// The description is the ONE thing this tool does for a Go action, so it is
// recorded rather than merely allowed: a test that asserts only on the returned
// error cannot tell a build that described the action from one that skipped
// straight past it, which is the state this path was in.
func withGoBuildHarness(t *testing.T, describeErr error) *goBuildHarness {
	t.Helper()
	h := &goBuildHarness{}

	origDetect := DetectActionLanguageFunc
	origExtract := ExtractMetadataFunc
	origParseEnv := ParseExecutionEnvironmentFunc
	t.Cleanup(func() {
		DetectActionLanguageFunc = origDetect
		ExtractMetadataFunc = origExtract
		ParseExecutionEnvironmentFunc = origParseEnv
	})

	ExtractMetadataFunc = func(fs fsx.FileSystem, actionDir string) error {
		h.mu.Lock()
		h.described = append(h.described, actionDir)
		h.mu.Unlock()

		return describeErr
	}
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	return h
}

// A GO ACTION IS DESCRIBED FROM ITS OWN SOURCE, BY THE BUILD, LIKE EVERY OTHER
// ACTION.
//
// It was not. The build answered "the platform compiles Go" and returned before
// the description step, so this tool wrote no action.json for a Go action —
// leaving whatever an earlier run had put beside it, or nothing at all, which
// the platform's own build reads as an action that has never been built. A whole
// app written in Go could be built here with no file describing any of it.
func TestBuildAction_Go_DescribesTheActionFromItsSource(t *testing.T) {
	h := withGoBuildHarness(t, nil)

	actionDir := goAction(t, t.TempDir())
	m := NewBuildManager(DefaultBuildOptions())

	m.BuildAction(context.Background(), actionDir, nil)

	if len(h.described) != 1 {
		t.Fatalf("the description step ran %d times for a Go action, want once", len(h.described))
	}
	if h.described[0] != actionDir {
		t.Errorf("the description step was given %s, want the action's own directory %s", h.described[0], actionDir)
	}
}

// The build says what it did and what it did not, in one sentence, because a
// failure that names only the missing module reads as a build where nothing
// happened — and the file it did write is the one a developer is about to
// commit.
func TestBuildAction_Go_NamesBothHalvesOfWhatHappened(t *testing.T) {
	withGoBuildHarness(t, nil)

	actionDir := goAction(t, t.TempDir())
	m := NewBuildManager(DefaultBuildOptions())

	result := m.BuildAction(context.Background(), actionDir, nil)
	if result.Error == nil {
		t.Fatal("BuildAction() error = nil, want a Go action to be refused rather than reported as built")
	}

	for _, want := range []string{"Go", "action.json", "platform"} {
		if !strings.Contains(result.Error.Error(), want) {
			t.Errorf("BuildAction() error = %q, want it to name %q", result.Error, want)
		}
	}
}

// A GO ACTION THAT CANNOT BE DESCRIBED DOES NOT BUILD, AND THE REFUSAL IS WHAT
// ITS AUTHOR IS TOLD.
//
// The generator refuses a malformed exposure statement and takes the action.json
// generated from the earlier source with it. Handing back this path's own
// sentence instead would tell an author their action is written in Go — which
// they know — while the mistake they have to fix, and the fact that their
// action is now described by nothing, went unmentioned.
func TestBuildAction_Go_RefusalReachesTheAuthorWhole(t *testing.T) {
	refusal := &AnnotationRefusal{
		Refusal: `sync-orders: @tool is a modifier tag and takes no value, but was written with "true"`,
	}
	withGoBuildHarness(t, refusal)

	actionDir := goAction(t, t.TempDir())
	m := NewBuildManager(DefaultBuildOptions())

	result := m.BuildAction(context.Background(), actionDir, nil)
	if result.Error == nil {
		t.Fatal("BuildAction() error = nil, want the refusal (a malformed annotation must fail the build)")
	}

	if !errors.Is(result.Error, error(refusal)) {
		t.Errorf("BuildAction() error = %v, want it to carry the refusal", result.Error)
	}
	if !strings.Contains(result.Error.Error(), "modifier tag and takes no value") {
		t.Errorf("BuildAction() error = %q, want the refusal text an author can act on", result.Error)
	}
	if strings.Contains(result.Error.Error(), "action.json was written") {
		t.Errorf("BuildAction() error = %q, want the refusal rather than a claim that the action was described", result.Error)
	}
}

// TestBuildAction_Go_WritesNoModule pins that a Go action is refused rather than
// reported as built, and that nothing here pretends to compile one. It is
// discovered like any other action, and a build that reports it as done leaves a
// developer waiting on an artifact this CLI was never going to write.
func TestBuildAction_Go_WritesNoModule(t *testing.T) {
	withGoBuildHarness(t, nil)

	actionDir := goAction(t, t.TempDir())
	m := NewBuildManager(DefaultBuildOptions())

	result := m.BuildAction(context.Background(), actionDir, nil)
	if result.Error == nil {
		t.Fatal("Expected an error for a Go action, got nil")
	}
	if fileExists(filepath.Join(actionDir, "build")) {
		t.Error("A build directory was created for an action this CLI does not compile")
	}
}

// A LANGUAGE THE BUILD HAS NO PATH FOR IS REFUSED BY NAME.
//
// The detector and the build hold two lists of languages, and the day they
// disagree the build must say so. Sending the odd one out down the last branch
// written would compile it with the wrong toolchain, or describe it with
// nothing, and either way the author finds out at deploy time.
func TestBuildAction_RefusesALanguageItHasNoPathFor(t *testing.T) {
	withGoBuildHarness(t, nil)
	DetectActionLanguageFunc = func(actionDir string) (ActionLanguage, error) { return ActionLanguage("cobol"), nil }

	actionDir := goAction(t, t.TempDir())
	m := NewBuildManager(DefaultBuildOptions())

	result := m.BuildAction(context.Background(), actionDir, nil)
	if result.Error == nil {
		t.Fatal("BuildAction() error = nil, want a language the build has no path for to be refused")
	}
	if !strings.Contains(result.Error.Error(), "cobol") {
		t.Errorf("BuildAction() error = %q, want it to name the language it has no path for", result.Error)
	}
}

// THE FILE IS THE PROOF, NOT THE CALL.
//
// The tests above stand in for the description step, so they prove the build
// reaches it. This one runs the real generator through the real build path and
// reads what landed on disk, because "action.json is written for a Go action" is
// a statement about a file — and a seam that is called and produces nothing
// reads identically to one that is not called at all.
func TestBuildAction_Go_WritesActionJSON(t *testing.T) {
	requireGenerator(t)

	origParseEnv := ParseExecutionEnvironmentFunc
	t.Cleanup(func() { ParseExecutionEnvironmentFunc = origParseEnv })
	ParseExecutionEnvironmentFunc = func(parser, dir string) (string, error) { return "server", nil }

	actionDir := writeGoAction(t, "sync-orders", `package main

// Syncs orders.
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	m := NewBuildManager(DefaultBuildOptions())

	// The build refuses a Go action either way — the module is not produced here
	// — so what it did is read off the file rather than off the outcome.
	m.BuildAction(context.Background(), actionDir, nil)

	if !fileExists(filepath.Join(actionDir, "action.json")) {
		t.Fatal("the build wrote no action.json for a Go action")
	}

	if description := generatedActionMetadata(t, actionDir).description(); description != "Syncs orders." {
		t.Errorf("action.json describes the action as %q, want what its source says", description)
	}
}
