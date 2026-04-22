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
	srcDir := filepath.Join(actionDir, "src")

	if err := os.MkdirAll(srcDir, 0755); err != nil {
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
  
  /** Optional custom message */
  message?: string;
  
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

	tsPath := filepath.Join(srcDir, "index.ts")
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

	// The schema uses $ref format, so we need to check the definitions
	if metadata.Schema.Type == "" && len(metadata.Schema.Definitions) > 0 {
		// Schema uses $ref format - check the Payload definition
		payloadDef, ok := metadata.Schema.Definitions["Payload"]
		if !ok {
			t.Fatal("Payload definition not found in schema")
		}

		if payloadDef.Type != "object" {
			t.Errorf("Payload type = %s, want object", payloadDef.Type)
		}

		// Check that preferences is properly nested
		preferences, ok := payloadDef.Properties["preferences"]
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

		// Verify newsletter and frequency are nested
		if _, ok := preferences.Properties["newsletter"]; !ok {
			t.Error("newsletter should be nested under preferences")
		}
		if _, ok := preferences.Properties["frequency"]; !ok {
			t.Error("frequency should be nested under preferences")
		}
	} else if metadata.Schema.Type == "object" {
		// Inline schema format
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
	} else {
		t.Error("Schema should be either inline object or use $ref format")
	}
}
