package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simple-cli/internal/fsx"
)

// generatedActionMetadata is what the generator left on disk, read back by the
// test rather than by the extractor.
//
// The extractor no longer re-renders the file through these structs — they
// model a subset of JSON Schema, and the CLI's copy of an action's contract has
// to be the generator's bytes rather than what survives a round trip through a
// narrower model. A test may still decode it, because the fixtures here are
// within that subset and a test that decodes is not a build that rewrites.
func generatedActionMetadata(t *testing.T, actionDir string) ActionMetadata {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(actionDir, "action.json"))
	if err != nil {
		t.Fatalf("Failed to read action.json: %v", err)
	}

	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("action.json should end with a newline")
	}

	var metadata ActionMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("Failed to decode action.json: %v", err)
	}

	return metadata
}

// TestExtractTypeScriptMetadata_Integration tests the Node.js-based extraction
// This test requires Node.js to be installed and will install npm packages if needed
func TestExtractTypeScriptMetadata_Integration(t *testing.T) {
	// Skip if Node.js is not available
	if err := checkNodeJS(); err != nil {
		t.Skip("Node.js not available, skipping integration test")
	}

	// Create a temporary action directory
	tmpDir := t.TempDir()
	actionDir := filepath.Join(tmpDir, "test-action")

	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatalf("Failed to create test action directory: %v", err)
	}

	// Write a test TypeScript file
	tsContent := `/**
 * Sends a welcome email to a new user
 * Integrates with SendGrid API
 */
export interface Payload {
  /** User's email address (must be valid) */
  email: string;
  
  /** User's display name */
  name: string;
  
  /**
   * Optional custom message
   * @maxLength 280
   */
  message?: string;

  /** Open metadata dictionary */
  metadata?: Record<string, unknown>;
  
  /** User's age (18-120) */
  age?: number;
  
  /** Email preferences */
  preferences?: {
    /** Send newsletter */
    newsletter: boolean;
    
    /** Email frequency (daily, weekly, monthly) */
    frequency: string;
  };
}

export async function handler(req: any): Promise<{ success: boolean }> {
  // Implementation
  return { success: true };
}
`

	tsPath := filepath.Join(actionDir, "index.ts")
	if err := os.WriteFile(tsPath, []byte(tsContent), 0644); err != nil {
		t.Fatalf("Failed to write test TypeScript file: %v", err)
	}

	// Write a minimal tsconfig.json
	tsconfigContent := `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "commonjs",
    "strict": true,
    "esModuleInterop": true
  }
}
`
	tsconfigPath := filepath.Join(actionDir, "tsconfig.json")
	if err := os.WriteFile(tsconfigPath, []byte(tsconfigContent), 0644); err != nil {
		t.Fatalf("Failed to write tsconfig.json: %v", err)
	}

	// Extract metadata
	fs := fsx.OSFileSystem{}
	if err := extractTypeScriptMetadata(fs, actionDir); err != nil {
		t.Fatalf("extractTypeScriptMetadata failed: %v", err)
	}

	metadata := generatedActionMetadata(t, actionDir)

	// Verify the metadata
	if metadata.Description == "" {
		t.Error("Description should not be empty")
	}

	if metadata.Schema.Ref != "" || len(metadata.Schema.Definitions) > 0 {
		t.Fatalf("schema should be root-inlined, got ref=%q definitions=%d", metadata.Schema.Ref, len(metadata.Schema.Definitions))
	}

	if metadata.Schema.Type != "object" {
		t.Fatalf("schema type = %s, want object", metadata.Schema.Type)
	}

	// Check that preferences is properly nested
	preferences, ok := metadata.Schema.Properties["preferences"]
	if !ok {
		t.Fatal("preferences property not found")
	}

	if preferences.Type != "object" {
		t.Errorf("preferences type = %s, want object", preferences.Type)
	}

	// Check nested properties
	if len(preferences.Properties) == 0 {
		t.Error("preferences should have nested properties")
	}

	message, ok := metadata.Schema.Properties["message"]
	if !ok {
		t.Fatal("message property not found")
	}

	if message.MaxLength == nil || *message.MaxLength != 280 {
		t.Fatalf("message maxLength = %v, want 280", message.MaxLength)
	}

	openMetadata, ok := metadata.Schema.Properties["metadata"]
	if !ok {
		t.Fatal("metadata property not found")
	}

	if openMetadata.AdditionalProperties != true {
		t.Fatalf("metadata additionalProperties = %#v, want true", openMetadata.AdditionalProperties)
	}
}

func TestExtractTypeScriptMetadata_NoPayloadUsesNoInputSchema(t *testing.T) {
	// Skip if Node.js is not available
	if err := checkNodeJS(); err != nil {
		t.Skip("Node.js not available, skipping integration test")
	}

	tmpDir := t.TempDir()
	actionDir := filepath.Join(tmpDir, "test-action")

	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatalf("Failed to create test action directory: %v", err)
	}

	tsPath := filepath.Join(actionDir, "index.ts")
	tsContent := `/**
 * Runs without input
 */
export async function handler(): Promise<{ success: boolean }> {
  return { success: true };
}
`

	if err := os.WriteFile(tsPath, []byte(tsContent), 0644); err != nil {
		t.Fatalf("Failed to write test TypeScript file: %v", err)
	}

	fs := fsx.OSFileSystem{}
	if err := extractTypeScriptMetadata(fs, actionDir); err != nil {
		t.Fatalf("extractTypeScriptMetadata failed: %v", err)
	}

	metadata := generatedActionMetadata(t, actionDir)

	if metadata.Schema.Type != "object" {
		t.Fatalf("schema type = %s, want object", metadata.Schema.Type)
	}

	if metadata.Schema.Properties == nil || len(metadata.Schema.Properties) != 0 {
		t.Fatalf("schema properties = %#v, want empty object", metadata.Schema.Properties)
	}

	if metadata.Schema.AdditionalProperties != false {
		t.Fatalf("schema additionalProperties = %#v, want false", metadata.Schema.AdditionalProperties)
	}
}

// A SHAPE THIS PACKAGE CANNOT MODEL IS STILL THE ACTION'S CONTRACT.
//
// A member typed as a union renders `"type": ["string", "null"]`. The structs
// in this package hold a type as one string, so re-rendering the generator's
// output through them stopped the build dead on a source the platform's own
// generator describes without complaint — two tools disagreeing about whether
// the same action builds at all.
//
// So the generator's bytes are left alone, and this holds them to it: what the
// CLI leaves on disk is what the generator wrote, byte for byte.
func TestExtractTypeScriptMetadataLeavesTheGeneratorsBytesAlone(t *testing.T) {
	if err := checkNodeJS(); err != nil {
		t.Skip("Node.js not available, skipping integration test")
	}

	actionDir := filepath.Join(t.TempDir(), "test-action")
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatalf("Failed to create test action directory: %v", err)
	}

	tsContent := `/**
 * Reads a register.
 */
export interface Payload {
  /** One site, or null when scoping by company. */
  site_id: string | null;
}

export async function handler(): Promise<{ ok: boolean }> {
  return { ok: true };
}
`

	if err := os.WriteFile(filepath.Join(actionDir, "index.ts"), []byte(tsContent), 0644); err != nil {
		t.Fatalf("Failed to write test TypeScript file: %v", err)
	}

	fs := fsx.OSFileSystem{}
	if err := extractTypeScriptMetadata(fs, actionDir); err != nil {
		t.Fatalf("a member typed as a union stopped the extraction: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(actionDir, "action.json"))
	if err != nil {
		t.Fatalf("Failed to read action.json: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(written, &decoded); err != nil {
		t.Fatalf("Failed to decode action.json: %v", err)
	}

	siteID := decoded["schema"].(map[string]any)["properties"].(map[string]any)["site_id"].(map[string]any)
	if _, union := siteID["type"].([]any); !union {
		t.Fatalf("the union type did not survive: %#v", siteID["type"])
	}

	// Running the extractor again over the same source must not change a byte.
	// A rewrite that reorders keys or drops what it cannot hold shows up here.
	if err := extractTypeScriptMetadata(fs, actionDir); err != nil {
		t.Fatalf("second extraction failed: %v", err)
	}

	again, err := os.ReadFile(filepath.Join(actionDir, "action.json"))
	if err != nil {
		t.Fatalf("Failed to read action.json: %v", err)
	}

	if string(again) != string(written) {
		t.Fatalf("the generated contract is not stable across runs:\n%s\n---\n%s", written, again)
	}
}

// THE SAME TWO PROPERTIES FOR THE TYPESCRIPT PATH, WHICH IS A DIFFERENT
// GENERATOR IN A CHILD PROCESS.
//
// The two describe one authoring vocabulary, so they have to agree about one
// doc block. They did not: the TypeScript one read the description as the text
// before the first tag, which a TypeScript parser hands back with everything
// after that tag folded into the tag's own comment — so a paragraph written
// after the annotations was deleted from the text a model reads, silently, on a
// build that exited zero. And it read annotations only from the block that
// supplied the description, so a statement written anywhere else in the file
// left the action quietly not a tool.
func TestExtractTypeScriptMetadataKeepsWhatIsWrittenAroundTheAnnotations(t *testing.T) {
	if err := checkNodeJS(); err != nil {
		t.Skip("Node.js not available, skipping integration test")
	}

	actionDir := filepath.Join(t.TempDir(), "query-things")
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatalf("Failed to create test action directory: %v", err)
	}

	tsContent := `/**
 * The things module.
 *
 * @tool
 * @effects read
 * @retry safe
 */

import simple from '@simple/sdk'

/**
 * Reads things.
 *
 * A name matching no row is REFUSED rather than answered with an empty
 * result, so an empty answer is never false good news.
 */
export interface Payload {
  name: string;
}

simple.Handle(() => ({ ok: true }))
`

	if err := os.WriteFile(filepath.Join(actionDir, "index.ts"), []byte(tsContent), 0644); err != nil {
		t.Fatalf("Failed to write test TypeScript file: %v", err)
	}

	if err := extractTypeScriptMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	metadata := generatedActionMetadata(t, actionDir)

	if metadata.AI == nil || !metadata.AI.Tool {
		t.Fatalf("an exposure statement written outside the describing block was dropped: %#v", metadata.AI)
	}

	if !strings.Contains(metadata.Description, "REFUSED rather than answered") {
		t.Fatalf("the description lost the rule written in it, got %q", metadata.Description)
	}

	if strings.Contains(metadata.Description, "@") {
		t.Fatalf("expected every annotation to be lifted out of the description, got %q", metadata.Description)
	}
}
