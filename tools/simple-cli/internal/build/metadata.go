package build

import (
	"errors"
	"fmt"
	"path/filepath"

	"simple-cli/internal/fsx"
)

// ExtractMetadata generates action.json from an action's own source.
//
// FAILING IS FATAL TO THE BUILD: what comes out of here is the action's
// description, its input schema, and its statement about whether an agent may
// call it, so a build that could not produce them has verified nothing about any
// of the three — and the build carries on to package artifacts that the file
// beside them no longer describes.
//
// ONE GENERATOR DESCRIBES AN ACTION, AND THIS TOOL RUNS IT RATHER THAN ITS OWN
// READING OF THE SAME CONTRACT. A Go action used to be described by seven
// hundred lines of Go living in this package, parsing the same doc comments and
// the same struct tags as the generator the platform runs. Two implementations
// of one contract is two contracts: they had already drifted — one joined a
// description's lines with spaces where the other kept the newlines, and one
// read a payload field's annotations where the other did not — and an author
// building through this tool got a different file from the same source.
//
// Nothing here could reach that code. The build refuses an action without
// `src/index.ts` before extraction runs, and a third party authors in
// TypeScript, so what it produced was a second contract that could never be
// exercised or contradicted.
//
// The language is still detected, because THAT question this tool must answer:
// an action with both sources or neither has a problem the generator would
// describe as a missing root type, and an empty action.json is worse than a
// refusal.
//
// When the failure is a refusal — the source says something the exposure
// vocabulary does not admit — the action.json already on disk is discarded
// with it. That file was generated from an earlier source; leaving it is how a
// rejected edit still ships, because every later reader sees a well-formed file
// and nothing that says which source it came from.
func ExtractMetadata(fs fsx.FileSystem, actionDir string) error {
	if _, err := detectLanguage(fs, actionDir); err != nil {
		return fmt.Errorf("failed to detect action language: %w", err)
	}

	if err := describeActionFromSource(fs, actionDir); err != nil {
		return discardStaleActionJSON(fs, actionDir, err)
	}

	return nil
}

// discardStaleActionJSON removes a generated action.json that the source it was
// generated from has since made untrue, and hands back the refusal that made it
// so.
//
// Only a refusal discards. A generator that could not run leaves the file
// alone: it may still be a faithful description, and deleting every action's
// metadata because `node` is missing would turn one environment problem into a
// working tree nobody can build from.
func discardStaleActionJSON(fs fsx.FileSystem, actionDir string, cause error) error {
	var refusal *AnnotationRefusal
	if !errors.As(cause, &refusal) {
		return cause
	}

	if err := fs.Remove(filepath.Join(actionDir, "action.json")); err != nil {
		return fmt.Errorf("%w (and its stale action.json could not be removed: %v)", cause, err)
	}

	return cause
}

// describeActionFromSource is implemented in metadata_generator.go

// detectLanguage determines if action is TypeScript or Go based on source file presence.
// Returns "typescript" if index.ts or src/index.ts exists, "go" if main.go exists.
// Returns error if both files exist (ambiguous) or neither exists (missing source).
func detectLanguage(fs fsx.FileSystem, actionDir string) (string, error) {
	rootTSPath := filepath.Join(actionDir, "index.ts")
	srcTSPath := filepath.Join(actionDir, "src", "index.ts")
	goPath := filepath.Join(actionDir, "main.go")

	// Check for TypeScript source
	_, rootTSErr := fs.Stat(rootTSPath)
	rootTSExists := rootTSErr == nil

	_, srcTSErr := fs.Stat(srcTSPath)
	srcTSExists := srcTSErr == nil

	tsExists := rootTSExists || srcTSExists

	// Check for Go source
	_, goErr := fs.Stat(goPath)
	goExists := goErr == nil

	// Handle ambiguous case (both files present)
	if tsExists && goExists {
		return "", fmt.Errorf("ambiguous action language: TypeScript source (index.ts or src/index.ts) and main.go found")
	}

	// Handle missing source case (neither file present)
	if !tsExists && !goExists {
		return "", fmt.Errorf("no action source file found (expected index.ts, src/index.ts, or main.go)")
	}

	// Return detected language
	if tsExists {
		return "typescript", nil
	}
	return "go", nil
}
