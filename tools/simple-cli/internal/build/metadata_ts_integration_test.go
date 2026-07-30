package build

import (
	"os"
	"path/filepath"
	"testing"

	"simple-cli/internal/fsx"
)

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
	metadata, err := extractTypeScriptMetadata(fs, actionDir)
	if err != nil {
		t.Fatalf("extractTypeScriptMetadata failed: %v", err)
	}

	// Verify action.json was created with trailing newline
	actionJSONPath := filepath.Join(actionDir, "action.json")
	data, err := os.ReadFile(actionJSONPath)
	if err != nil {
		t.Fatalf("Failed to read action.json: %v", err)
	}

	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("action.json should end with a newline")
	}

	t.Logf("Generated action.json:\n%s", string(data))

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
	metadata, err := extractTypeScriptMetadata(fs, actionDir)
	if err != nil {
		t.Fatalf("extractTypeScriptMetadata failed: %v", err)
	}

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
