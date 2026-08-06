package build

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"simple-cli/internal/fsx"
)

// A GENERATOR READS SOURCE AND WRITES ONLY ITS ARTIFACT.
//
// The description, input schema and exposure statement in action.json are
// derived from an action's own .ts or .go file. Nothing about that is allowed to
// run in the other direction: a build that edits the source it parsed rewrites
// an author's file to suit its own parser, and the author's next diff carries a
// change nobody made. For a third party, that file is not even ours to touch.
//
// It is also how the honest fixes get skipped. Every defect these extractors
// have had — a description one parser truncated, an annotation another dropped —
// has a fix that strips or rewrites the source until the parser agrees, and it
// is always the shortest one to write. Asserted here, the shortest fix stops
// compiling.
//
// Both languages, because they are two different extractors: one shells out to
// a Node generator, the other parses in this process.
func TestExtractMetadataLeavesEverySourceFileAlone(t *testing.T) {
	if err := checkNodeJS(); err != nil {
		t.Skip("Node.js not available, skipping integration test")
	}

	tsAction := filepath.Join(t.TempDir(), "query-things")
	writeSourceFiles(t, tsAction, map[string]string{
		"index.ts": `/**
 * Reads things.
 *
 * @tool
 * @effects read
 * @retry safe
 *
 * A name matching no row is REFUSED rather than answered with an empty
 * result, so an empty answer is never false good news.
 */
export interface Payload {
  name: string;
}
`,
		"runner.mjs": "export function run() { return { ok: true } }\n",
	})

	goAction := filepath.Join(t.TempDir(), "mutate-things")
	writeSourceFiles(t, goAction, map[string]string{
		"main.go": "package main\n\n" +
			"// Writes things.\n" +
			"//\n" +
			"// @tool\n" +
			"// @effects write, destructive\n" +
			"// @retry never\n" +
			"//\n" +
			"// @Payload Input\n" +
			"func handler() {}\n\n" +
			"type Input struct {\n" +
			"\tName string `json:\"name\" jsonschema:\"required\"`\n" +
			"}\n",
	})

	fs := fsx.OSFileSystem{}

	for _, actionDir := range []string{tsAction, goAction} {
		before := sourceHashes(t, actionDir)

		if len(before) == 0 {
			t.Fatalf("%s has no source files, so this proves nothing", actionDir)
		}

		if err := ExtractMetadata(fs, actionDir); err != nil {
			t.Fatalf("expected %s to be described, got %v", actionDir, err)
		}

		for name, hash := range sourceHashes(t, actionDir) {
			if before[name] != hash {
				t.Fatalf("describing %s rewrote %s, a file the action's author owns", actionDir, name)
			}
		}

		if len(sourceHashes(t, actionDir)) != len(before) {
			t.Fatalf("describing %s added or removed a source file", actionDir)
		}
	}
}

func writeSourceFiles(t *testing.T, actionDir string, files map[string]string) {
	t.Helper()

	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatalf("Failed to create %s: %v", actionDir, err)
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(actionDir, name), []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", name, err)
		}
	}
}

// Every file in the action an author wrote, by content.
//
// Hashed rather than watched: a write that restores the same bytes is not a
// modification, and one that leaves different bytes is caught whatever made it —
// the extractor, a toolchain it shells out to, or a formatter either one runs.
func sourceHashes(t *testing.T, actionDir string) map[string]string {
	t.Helper()

	hashes := map[string]string{}

	err := filepath.WalkDir(actionDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		switch filepath.Ext(path) {
		case ".ts", ".go", ".js", ".mjs":
		default:
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		name, relErr := filepath.Rel(actionDir, path)
		if relErr != nil {
			return relErr
		}

		sum := sha256.Sum256(content)
		hashes[name] = hex.EncodeToString(sum[:])

		return nil
	})
	if err != nil {
		t.Fatalf("Failed to hash the sources under %s: %v", actionDir, err)
	}

	return hashes
}
