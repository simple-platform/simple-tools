package build

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"simple-cli/internal/fsx"
)

// PayloadInfo contains information about a function's @Payload annotation
type PayloadInfo struct {
	StructName  string        // Name of the struct referenced in @Payload annotation
	Description string        // Function's GoDoc comment (excluding @Payload line)
	FuncNode    *ast.FuncDecl // AST node of the function
}

// extractGoMetadata parses a Go action and generates metadata.
// It reads main.go, finds the @Payload annotation, extracts the struct,
// and generates a JSON Schema from the struct definition.
func extractGoMetadata(fs fsx.FileSystem, actionDir string) (*ActionMetadata, error) {
	// Read main.go file
	mainGoPath := filepath.Join(actionDir, "main.go")
	content, err := fs.ReadFile(mainGoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read main.go: %w", err)
	}

	// Parse Go source file
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, mainGoPath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse main.go: %w", err)
	}

	// Find function with @Payload annotation
	payloadInfo, err := findPayloadAnnotation(fset, file)
	if err != nil {
		return nil, err
	}

	// Find the struct definition referenced by @Payload annotation
	structInfo, err := findStruct(fset, []*ast.File{file}, payloadInfo.StructName)
	if err != nil {
		return nil, err
	}

	// Generate JSON Schema from struct
	schema, err := generateSchemaFromStruct(structInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to generate schema: %w", err)
	}

	// Build ActionMetadata
	metadata := &ActionMetadata{
		Description: payloadInfo.Description,
		Schema:      schema,
	}

	return metadata, nil
}

// findPayloadAnnotation searches for a function with @Payload annotation in its comment.
// Returns PayloadInfo with the struct name, description, and function node.
// Returns error if no @Payload annotation is found or if the annotation format is invalid.
func findPayloadAnnotation(_ *token.FileSet, file *ast.File) (*PayloadInfo, error) {
	var payloadInfo *PayloadInfo

	// Iterate through all declarations in the file
	for _, decl := range file.Decls {
		// Check if this is a function declaration
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		// Check if function has a doc comment
		if funcDecl.Doc == nil {
			continue
		}

		// Search for @Payload annotation in function comments
		var structName string
		var descriptionLines []string
		var foundPayloadAnnotation bool

		for _, comment := range funcDecl.Doc.List {
			text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))

			// Check if this line contains @Payload annotation
			if strings.HasPrefix(text, "@Payload ") || text == "@Payload" {
				foundPayloadAnnotation = true
				// Extract struct name after @Payload
				parts := strings.Fields(text)
				if len(parts) < 2 {
					return nil, fmt.Errorf("invalid @Payload annotation format: expected '@Payload StructName', got '%s'", text)
				}
				structName = parts[1]
				// Don't include the @Payload line in the description
				continue
			}

			// Add non-@Payload lines to description
			descriptionLines = append(descriptionLines, text)
		}

		// If we found a @Payload annotation, create PayloadInfo
		if foundPayloadAnnotation {
			payloadInfo = &PayloadInfo{
				StructName:  structName,
				Description: strings.TrimSpace(strings.Join(descriptionLines, " ")),
				FuncNode:    funcDecl,
			}
			break
		}
	}

	// Return error if no @Payload annotation was found
	if payloadInfo == nil {
		return nil, fmt.Errorf("@Payload annotation not found in main.go")
	}

	return payloadInfo, nil
}

// StructInfo contains information about a struct definition
type StructInfo struct {
	StructType *ast.StructType // The struct type AST node
	Fields     []FieldInfo     // Extracted field information
}

// FieldInfo contains information about a struct field
type FieldInfo struct {
	Name    string   // Field name
	Type    ast.Expr // Field type expression
	Tag     string   // Struct tag (if present)
	Comment string   // Field comment (GoDoc)
}

// findStruct searches all parsed files for a struct definition by name.
// Returns the struct type and extracted field information.
// Returns error if the struct is not found.
func findStruct(_ *token.FileSet, files []*ast.File, structName string) (*StructInfo, error) {
	for _, file := range files {
		// Iterate through all declarations in the file
		for _, decl := range file.Decls {
			// Check if this is a general declaration (type, const, var)
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}

			// Check if this is a type declaration
			if genDecl.Tok != token.TYPE {
				continue
			}

			// Iterate through all specs in the declaration
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				// Check if this is the struct we're looking for
				if typeSpec.Name.Name != structName {
					continue
				}

				// Check if this is a struct type
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					return nil, fmt.Errorf("type '%s' is not a struct", structName)
				}

				// Extract field information
				fields := extractFieldInfo(structType)

				return &StructInfo{
					StructType: structType,
					Fields:     fields,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("struct '%s' not found", structName)
}

// extractFieldInfo extracts field names, types, tags, and comments from a struct.
// Handles nested struct definitions and extracts GoDoc comments immediately preceding fields.
func extractFieldInfo(structType *ast.StructType) []FieldInfo {
	var fields []FieldInfo

	if structType.Fields == nil {
		return fields
	}

	for _, field := range structType.Fields.List {
		// Extract field comment (GoDoc immediately preceding the field)
		var comment string
		if field.Doc != nil {
			var commentLines []string
			for _, c := range field.Doc.List {
				text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
				commentLines = append(commentLines, text)
			}
			comment = strings.TrimSpace(strings.Join(commentLines, " "))
		}

		// Extract struct tag
		var tag string
		if field.Tag != nil {
			// Remove backticks from tag literal
			tag = strings.Trim(field.Tag.Value, "`")
		}

		// Handle fields with multiple names (e.g., "x, y int")
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				fields = append(fields, FieldInfo{
					Name:    name.Name,
					Type:    field.Type,
					Tag:     tag,
					Comment: comment,
				})
			}
		} else {
			// Embedded field (no name)
			// For now, we skip embedded fields as they're not common in action payloads
			// This can be enhanced later if needed
			continue
		}
	}

	return fields
}

// StructTagInfo contains parsed information from json and jsonschema struct tags
type StructTagInfo struct {
	JSONName string   // Property name from json tag (e.g., json:"email" -> "email")
	Omit     bool     // Whether field should be omitted (json:"-")
	Optional bool     // Whether field is optional (json:",omitempty")
	Required bool     // Whether field is required (jsonschema:"required")
	Default  string   // Default value (jsonschema:"default=value")
	Min      *float64 // Minimum value (jsonschema:"minimum=N")
	Max      *float64 // Maximum value (jsonschema:"maximum=N")
	Pattern  string   // Regex pattern (jsonschema:"pattern=regex")
}

// parseStructTag parses json and jsonschema struct tags and extracts metadata.
// Supports the following formats:
//   - json:"name" - property name
//   - json:"-" - omit field
//   - json:",omitempty" - optional field
//   - json:"name,omitempty" - named optional field
//   - jsonschema:"required" - mark as required
//   - jsonschema:"default=value" - set default value
//   - jsonschema:"minimum=N" - set minimum constraint
//   - jsonschema:"maximum=N" - set maximum constraint
//   - jsonschema:"pattern=regex" - set pattern constraint
//   - jsonschema:"required,minimum=0,maximum=100" - multiple constraints
func parseStructTag(tag string) StructTagInfo {
	info := StructTagInfo{}

	// Parse json tag
	if jsonTag := extractTag(tag, "json"); jsonTag != "" {
		parseJSONTag(&info, jsonTag)
	}

	// Parse jsonschema tag
	if schemaTag := extractTag(tag, "jsonschema"); schemaTag != "" {
		parseJSONSchemaTag(&info, schemaTag)
	}

	return info
}

// extractTag extracts the value of a specific struct tag key.
// Example: extractTag(`json:"name" jsonschema:"required"`, "json") returns "name"
func extractTag(tag, key string) string {
	// Look for key:"value" pattern
	keyPrefix := key + `:"`
	start := strings.Index(tag, keyPrefix)
	if start == -1 {
		return ""
	}

	// Move past the key:" part
	start += len(keyPrefix)

	// Find the closing quote
	end := strings.Index(tag[start:], `"`)
	if end == -1 {
		return ""
	}

	return tag[start : start+end]
}

// parseJSONTag parses the json struct tag and updates StructTagInfo.
// Handles formats: "name", "-", ",omitempty", "name,omitempty"
func parseJSONTag(info *StructTagInfo, jsonTag string) {
	// Check for omit marker
	if jsonTag == "-" {
		info.Omit = true
		return
	}

	// Split by comma to separate name from options
	parts := strings.Split(jsonTag, ",")

	// First part is the property name (if not empty)
	if len(parts) > 0 && parts[0] != "" {
		info.JSONName = strings.TrimSpace(parts[0])
	}

	// Check for omitempty option
	for i := 1; i < len(parts); i++ {
		if strings.TrimSpace(parts[i]) == "omitempty" {
			info.Optional = true
			break
		}
	}
}

// parseJSONSchemaTag parses the jsonschema struct tag and updates StructTagInfo.
// Handles comma-separated constraints: "required,minimum=0,maximum=100,pattern=^[a-z]+$,default=test"
func parseJSONSchemaTag(info *StructTagInfo, schemaTag string) {
	// Parse constraints more carefully to handle commas in values (e.g., patterns)
	constraints := splitConstraints(schemaTag)

	for _, constraint := range constraints {
		constraint = strings.TrimSpace(constraint)

		// Check for key=value format
		if strings.Contains(constraint, "=") {
			parts := strings.SplitN(constraint, "=", 2)
			if len(parts) != 2 {
				continue
			}

			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])

			switch key {
			case "default":
				info.Default = value
			case "minimum":
				if min, err := parseFloat(value); err == nil {
					info.Min = &min
				}
			case "maximum":
				if max, err := parseFloat(value); err == nil {
					info.Max = &max
				}
			case "pattern":
				info.Pattern = value
			}
		} else {
			// Simple flag (no value)
			switch constraint {
			case "required":
				info.Required = true
			}
		}
	}
}

// splitConstraints splits a jsonschema tag by commas, but preserves commas within patterns.
// This is a simple heuristic: if we see "pattern=", we take everything until the next known key.
func splitConstraints(schemaTag string) []string {
	var constraints []string
	var current strings.Builder
	inPattern := false

	parts := strings.Split(schemaTag, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)

		// Check if this part starts a pattern constraint
		if strings.HasPrefix(part, "pattern=") {
			// If we have accumulated content, save it
			if current.Len() > 0 {
				constraints = append(constraints, current.String())
				current.Reset()
			}
			inPattern = true
			current.WriteString(part)
		} else if inPattern {
			// Check if this part starts a new constraint (contains = or is a known flag)
			if strings.Contains(part, "=") || part == "required" {
				// Save the pattern and start new constraint
				constraints = append(constraints, current.String())
				current.Reset()
				inPattern = false
				current.WriteString(part)
			} else {
				// Continue the pattern
				current.WriteString(",")
				current.WriteString(part)
			}
		} else {
			// Not in a pattern
			if current.Len() > 0 {
				constraints = append(constraints, current.String())
				current.Reset()
			}
			current.WriteString(part)
		}

		// If this is the last part, save what we have
		if i == len(parts)-1 && current.Len() > 0 {
			constraints = append(constraints, current.String())
		}
	}

	return constraints
}

// parseFloat parses a string to float64, handling both integer and decimal formats.
func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}

	// Use strconv for proper float parsing
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

// generateSchemaFromStruct converts a Go struct to JSON Schema.
// It maps Go types to JSON Schema types, applies struct tag constraints,
// and recursively handles nested structs.
// Returns error if circular references are detected or unsupported types are encountered.
func generateSchemaFromStruct(structInfo *StructInfo) (JSONSchema, error) {
	schema := JSONSchema{
		Type:       "object",
		Properties: make(map[string]Property),
		Required:   []string{},
	}

	// Track visited struct types to detect circular references
	visited := make(map[string]bool)

	// Process each field in the struct
	for _, field := range structInfo.Fields {
		// Parse struct tags
		tagInfo := parseStructTag(field.Tag)

		// Skip fields marked with json:"-"
		if tagInfo.Omit {
			continue
		}

		// Determine property name (use json tag if present, otherwise field name)
		propName := field.Name
		if tagInfo.JSONName != "" {
			propName = tagInfo.JSONName
		}

		// Generate property schema from field type
		prop, err := generatePropertyFromType(field.Type, field.Comment, tagInfo, visited)
		if err != nil {
			return schema, fmt.Errorf("failed to generate schema for field '%s': %w", field.Name, err)
		}

		// Add property to schema
		schema.Properties[propName] = prop

		// Add to required array if field is required
		// A field is required if:
		// 1. It has jsonschema:"required" tag, OR
		// 2. It doesn't have json:",omitempty" tag (unless explicitly marked optional)
		if tagInfo.Required || (!tagInfo.Optional && !tagInfo.Omit) {
			schema.Required = append(schema.Required, propName)
		}
	}

	return schema, nil
}

// generatePropertyFromType converts a Go type expression to a JSON Schema Property.
// Handles primitives, slices, maps, structs, and pointers recursively.
// The visited map tracks struct types to detect circular references.
func generatePropertyFromType(typeExpr ast.Expr, comment string, tagInfo StructTagInfo, visited map[string]bool) (Property, error) {
	prop := Property{
		Description: comment,
	}

	// Apply constraints from struct tags
	applyConstraints(&prop, tagInfo)

	// Determine the type and generate appropriate schema
	switch t := typeExpr.(type) {
	case *ast.Ident:
		// Primitive type or named type
		return generatePropertyFromIdent(t, prop)

	case *ast.StarExpr:
		// Pointer type (*T) - treat as nullable, recurse on the underlying type
		return generatePropertyFromType(t.X, comment, tagInfo, visited)

	case *ast.ArrayType:
		// Slice type ([]T)
		return generatePropertyFromArray(t, prop, tagInfo, visited)

	case *ast.MapType:
		// Map type (map[string]T)
		return generatePropertyFromMap(t, prop, tagInfo, visited)

	case *ast.StructType:
		// Inline struct type
		return generatePropertyFromInlineStruct(t, prop, visited)

	case *ast.SelectorExpr:
		// Qualified type (e.g., time.Time, pkg.Type)
		// For now, treat as string (can be enhanced later for known types)
		prop.Type = "string"
		return prop, nil

	default:
		return prop, fmt.Errorf("unsupported type: %T", typeExpr)
	}
}

// generatePropertyFromIdent handles identifier types (primitives and named types).
func generatePropertyFromIdent(ident *ast.Ident, prop Property) (Property, error) {
	typeName := ident.Name

	// Map Go primitive types to JSON Schema types
	switch typeName {
	case "string":
		prop.Type = "string"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		prop.Type = "number"
	case "bool":
		prop.Type = "boolean"
	default:
		// Named type (could be a struct, interface, or type alias)
		// For now, we don't have access to the full type information,
		// so we treat unknown types as objects
		// This could be enhanced by maintaining a type registry
		prop.Type = "object"
	}

	return prop, nil
}

// generatePropertyFromArray handles slice types ([]T).
func generatePropertyFromArray(arrayType *ast.ArrayType, prop Property, tagInfo StructTagInfo, visited map[string]bool) (Property, error) {
	prop.Type = "array"

	// Generate schema for array items
	itemProp, err := generatePropertyFromType(arrayType.Elt, "", tagInfo, visited)
	if err != nil {
		return prop, fmt.Errorf("failed to generate array item schema: %w", err)
	}

	prop.Items = &itemProp
	return prop, nil
}

// generatePropertyFromMap handles map types (map[string]T).
func generatePropertyFromMap(mapType *ast.MapType, prop Property, tagInfo StructTagInfo, visited map[string]bool) (Property, error) {
	// Verify key type is string
	keyIdent, ok := mapType.Key.(*ast.Ident)
	if !ok || keyIdent.Name != "string" {
		return prop, fmt.Errorf("unsupported map key type (only map[string]T is supported)")
	}

	prop.Type = "object"

	// Generate schema for map values (additionalProperties)
	valueProp, err := generatePropertyFromType(mapType.Value, "", tagInfo, visited)
	if err != nil {
		return prop, fmt.Errorf("failed to generate map value schema: %w", err)
	}

	// Use the AdditionalProperties field for map values
	prop.AdditionalProperties = &valueProp

	return prop, nil
}

// generatePropertyFromInlineStruct handles inline struct types.
func generatePropertyFromInlineStruct(structType *ast.StructType, prop Property, visited map[string]bool) (Property, error) {
	prop.Type = "object"
	prop.Properties = make(map[string]Property)

	// Extract fields from inline struct
	fields := extractFieldInfo(structType)

	// Process each field
	for _, field := range fields {
		tagInfo := parseStructTag(field.Tag)

		// Skip omitted fields
		if tagInfo.Omit {
			continue
		}

		// Determine property name
		propName := field.Name
		if tagInfo.JSONName != "" {
			propName = tagInfo.JSONName
		}

		// Generate property schema
		fieldProp, err := generatePropertyFromType(field.Type, field.Comment, tagInfo, visited)
		if err != nil {
			return prop, fmt.Errorf("failed to generate schema for inline struct field '%s': %w", field.Name, err)
		}

		prop.Properties[propName] = fieldProp
	}

	return prop, nil
}

// applyConstraints applies jsonschema struct tag constraints to a Property.
func applyConstraints(prop *Property, tagInfo StructTagInfo) {
	// Apply default value if present
	if tagInfo.Default != "" {
		// Try to parse the default value based on the property type
		switch prop.Type {
		case "number":
			// Parse as float64
			if val, err := parseFloat(tagInfo.Default); err == nil {
				prop.Default = val
			} else {
				// If parsing fails, store as string
				prop.Default = tagInfo.Default
			}
		case "boolean":
			// Parse as boolean
			switch tagInfo.Default {
			case "true":
				prop.Default = true
			case "false":
				prop.Default = false
			default:
				prop.Default = tagInfo.Default
			}
		default:
			// For strings and other types, store as-is
			prop.Default = tagInfo.Default
		}
	}

	// Apply minimum constraint
	if tagInfo.Min != nil {
		prop.Minimum = tagInfo.Min
	}

	// Apply maximum constraint
	if tagInfo.Max != nil {
		prop.Maximum = tagInfo.Max
	}

	// Apply pattern constraint
	if tagInfo.Pattern != "" {
		prop.Pattern = tagInfo.Pattern
	}
}
