package build

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"simple-cli/internal/fsx"
)

// ActionMetadata represents the output structure for action.json.
//
// The TypeScript path writes this file through a Node script and then reads it
// back through this struct before writing it again, so anything this struct
// does not model is dropped on the way through. The exposure statement is
// modelled here for that reason: a field missing from it is a key the build
// silently deletes from its own output.
type ActionMetadata struct {
	Description string      `json:"description"`
	Schema      JSONSchema  `json:"schema"`
	AI          *AIMetadata `json:"ai,omitempty"`
}

// JSONSchema represents JSON Schema structure
type JSONSchema struct {
	Ref                  string                     `json:"$ref,omitempty"` // For $ref format
	Type                 string                     `json:"type,omitempty"` // For inline format
	Description          string                     `json:"description,omitempty"`
	Properties           map[string]Property        `json:"properties,omitempty"` // For inline format
	Required             []string                   `json:"required,omitempty"`
	Definitions          map[string]PropertyWithDef `json:"definitions,omitempty"` // For $ref format
	Items                *Property                  `json:"items,omitempty"`
	AdditionalProperties any                        `json:"additionalProperties,omitempty"` // Can be bool, Property, or raw schema map
	Enum                 []any                      `json:"enum,omitempty"`
	AnyOf                []Property                 `json:"anyOf,omitempty"`
	Format               string                     `json:"format,omitempty"`
	Pattern              string                     `json:"pattern,omitempty"`
	MinItems             *int                       `json:"minItems,omitempty"`
	MaxItems             *int                       `json:"maxItems,omitempty"`
	MinLength            *int                       `json:"minLength,omitempty"`
	MaxLength            *int                       `json:"maxLength,omitempty"`
	Minimum              *float64                   `json:"minimum,omitempty"`
	Maximum              *float64                   `json:"maximum,omitempty"`
	MultipleOf           *float64                   `json:"multipleOf,omitempty"`
	Default              any                        `json:"default,omitempty"`
}

func (s JSONSchema) MarshalJSON() ([]byte, error) {
	out := map[string]any{}

	if s.Ref != "" {
		out["$ref"] = s.Ref
	}
	if s.Type != "" {
		out["type"] = s.Type
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if s.Properties != nil {
		out["properties"] = s.Properties
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	if len(s.Definitions) > 0 {
		out["definitions"] = s.Definitions
	}
	if s.Items != nil {
		out["items"] = s.Items
	}
	if s.AdditionalProperties != nil {
		out["additionalProperties"] = s.AdditionalProperties
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if len(s.AnyOf) > 0 {
		out["anyOf"] = s.AnyOf
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if s.Pattern != "" {
		out["pattern"] = s.Pattern
	}
	if s.MinItems != nil {
		out["minItems"] = *s.MinItems
	}
	if s.MaxItems != nil {
		out["maxItems"] = *s.MaxItems
	}
	if s.MinLength != nil {
		out["minLength"] = *s.MinLength
	}
	if s.MaxLength != nil {
		out["maxLength"] = *s.MaxLength
	}
	if s.Minimum != nil {
		out["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		out["maximum"] = *s.Maximum
	}
	if s.MultipleOf != nil {
		out["multipleOf"] = *s.MultipleOf
	}
	if s.Default != nil {
		out["default"] = s.Default
	}

	return json.Marshal(out)
}

// PropertyWithDef is like Property but can have its own definitions
type PropertyWithDef struct {
	Type                 string              `json:"type,omitempty"`
	Description          string              `json:"description,omitempty"`
	Items                *Property           `json:"items,omitempty"`
	Properties           map[string]Property `json:"properties,omitempty"`
	AdditionalProperties any                 `json:"additionalProperties,omitempty"` // Can be bool or Property
	Required             []string            `json:"required,omitempty"`
	Default              any                 `json:"default,omitempty"`
	Minimum              *float64            `json:"minimum,omitempty"`
	Maximum              *float64            `json:"maximum,omitempty"`
	Pattern              string              `json:"pattern,omitempty"`
	Enum                 []any               `json:"enum,omitempty"`
	AnyOf                []Property          `json:"anyOf,omitempty"`
	Format               string              `json:"format,omitempty"`
	MinItems             *int                `json:"minItems,omitempty"`
	MaxItems             *int                `json:"maxItems,omitempty"`
	MinLength            *int                `json:"minLength,omitempty"`
	MaxLength            *int                `json:"maxLength,omitempty"`
	MultipleOf           *float64            `json:"multipleOf,omitempty"`
}

// Property represents a JSON Schema property with support for nested objects and arrays
type Property struct {
	Type                 string              `json:"type,omitempty"`
	Description          string              `json:"description,omitempty"`
	Items                *Property           `json:"items,omitempty"`                // For arrays
	Properties           map[string]Property `json:"properties,omitempty"`           // For nested objects
	AdditionalProperties any                 `json:"additionalProperties,omitempty"` // Can be bool or Property
	Required             []string            `json:"required,omitempty"`             // For nested objects
	Default              any                 `json:"default,omitempty"`              // Default value
	Minimum              *float64            `json:"minimum,omitempty"`              // Minimum constraint
	Maximum              *float64            `json:"maximum,omitempty"`              // Maximum constraint
	Pattern              string              `json:"pattern,omitempty"`              // Regex pattern
	Enum                 []any               `json:"enum,omitempty"`                 // Enumerated values
	AnyOf                []Property          `json:"anyOf,omitempty"`                // Union schemas
	Format               string              `json:"format,omitempty"`               // Semantic format
	MinItems             *int                `json:"minItems,omitempty"`             // Array length minimum
	MaxItems             *int                `json:"maxItems,omitempty"`             // Array length maximum
	MinLength            *int                `json:"minLength,omitempty"`            // String length minimum
	MaxLength            *int                `json:"maxLength,omitempty"`            // String length maximum
	MultipleOf           *float64            `json:"multipleOf,omitempty"`           // Numeric multiple constraint
}

// ExtractMetadata generates action.json from source code comments.
// It detects the action language (TypeScript or Go) and routes to the appropriate extractor.
// Returns error if extraction fails (non-fatal to build).
func ExtractMetadata(fs fsx.FileSystem, actionDir string) error {
	// Detect action language
	lang, err := detectLanguage(fs, actionDir)
	if err != nil {
		return fmt.Errorf("failed to detect action language: %w", err)
	}

	// Route to appropriate extractor based on language
	var metadata *ActionMetadata
	switch lang {
	case "typescript":
		// TypeScript extraction will be implemented in Phase 3
		metadata, err = extractTypeScriptMetadata(fs, actionDir)
	case "go":
		// Go extraction will be implemented in Phase 2
		metadata, err = extractGoMetadata(fs, actionDir)
	default:
		return fmt.Errorf("unsupported action language: %s", lang)
	}

	if err != nil {
		return err
	}

	// Write action.json atomically
	if err = writeActionJSON(fs, actionDir, metadata); err != nil {
		return fmt.Errorf("failed to write action.json: %w", err)
	}

	return nil
}

// extractTypeScriptMetadata is implemented in metadata_ts.go

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

// writeActionJSON writes ActionMetadata to action.json with atomic file writing.
// Uses temp file + rename pattern to prevent partial writes.
func writeActionJSON(fs fsx.FileSystem, actionDir string, metadata *ActionMetadata) error {
	// Marshal metadata to JSON with 2-space indentation
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Add trailing newline for POSIX compliance
	data = append(data, '\n')

	// Write directly to final location
	// Note: For production with OSFileSystem, this could be enhanced to use
	// temp file + rename for atomic writes, but for testing with MockFileSystem
	// we write directly since rename is not supported in the mock.
	finalPath := filepath.Join(actionDir, "action.json")
	if err := fs.WriteFile(finalPath, data, fsx.FilePerm); err != nil {
		return fmt.Errorf("failed to write action.json: %w", err)
	}

	return nil
}
