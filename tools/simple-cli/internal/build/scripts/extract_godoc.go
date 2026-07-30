package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
)

type Schema struct {
	Type                 string            `json:"type,omitempty"`
	Description          string            `json:"description,omitempty"`
	Properties           map[string]Schema `json:"properties,omitempty"`
	Required             []string          `json:"required,omitempty"`
	Items                *Schema           `json:"items,omitempty"`
	Enum                 []any             `json:"enum,omitempty"`
	AnyOf                []Schema          `json:"anyOf,omitempty"`
	Format               string            `json:"format,omitempty"`
	Pattern              string            `json:"pattern,omitempty"`
	MinItems             *int              `json:"minItems,omitempty"`
	MaxItems             *int              `json:"maxItems,omitempty"`
	MinLength            *int              `json:"minLength,omitempty"`
	MaxLength            *int              `json:"maxLength,omitempty"`
	Minimum              *float64          `json:"minimum,omitempty"`
	Maximum              *float64          `json:"maximum,omitempty"`
	MultipleOf           *float64          `json:"multipleOf,omitempty"`
	Default              any               `json:"default,omitempty"`
	AdditionalProperties any               `json:"additionalProperties,omitempty"`
	ForceProperties      bool              `json:"-"`
}

type Output struct {
	Description string `json:"description"`
	Schema      Schema `json:"schema"`
}

type schemaParser struct {
	typeSpecs map[string]ast.Expr
	visiting  map[string]bool
}

type parsedSchemaTags struct {
	required                bool
	nullable                bool
	typeOverride            string
	enumValues              []string
	anyOfTypes              []string
	format                  string
	pattern                 string
	itemsType               string
	minItems                *int
	maxItems                *int
	minLength               *int
	maxLength               *int
	minimum                 *float64
	maximum                 *float64
	multipleOf              *float64
	defaultSet              bool
	defaultValue            any
	additionalPropertiesSet bool
	additionalProperties    bool
}

func (s Schema) MarshalJSON() ([]byte, error) {
	out := map[string]any{}

	if s.Type != "" {
		out["type"] = s.Type
	}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if s.Properties != nil && (len(s.Properties) > 0 || s.ForceProperties) {
		out["properties"] = s.Properties
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	if s.Items != nil {
		out["items"] = s.Items
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
	if s.AdditionalProperties != nil {
		out["additionalProperties"] = s.AdditionalProperties
	}

	return json.Marshal(out)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run extract_godoc.go [--] <file.go>")
		os.Exit(1)
	}

	filePath := os.Args[1]
	if filePath == "--" && len(os.Args) > 2 {
		filePath = os.Args[2]
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Parse error: %v\n", err)
		os.Exit(1)
	}

	var targetStruct string
	var overallDoc string

	// Pass 1: Find a function with @Payload annotation
	for _, decl := range node.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok {
			if fnDecl.Doc != nil {
				docText := fnDecl.Doc.Text()
				lines := strings.Split(docText, "\n")
				var descLines []string
				for _, line := range lines {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(trimmed, "@Payload") {
						parts := strings.Fields(trimmed)
						if len(parts) >= 2 {
							targetStruct = parts[1]
						}
					} else {
						descLines = append(descLines, line)
					}
				}
				if targetStruct != "" {
					overallDoc = strings.TrimSpace(strings.Join(descLines, "\n"))
					break
				}
			}
		}
	}

	// Pass 1.5: If no @Payload annotation exists, infer payload struct from req.Parse(&x).
	// This keeps schema generation resilient even when docs omit @Payload.
	if targetStruct == "" {
		if inferredStruct, inferredDoc := inferPayloadStruct(node); inferredStruct != "" {
			targetStruct = inferredStruct
			overallDoc = inferredDoc
		}
	}

	var payloadStruct *ast.StructType

	// Pass 2: Find the matching struct
	for _, decl := range node.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			if targetStruct != "" {
				if typeSpec.Name.Name != targetStruct {
					continue
				}
			} else {
				if typeSpec.Name.Name != "Input" && typeSpec.Name.Name != "Payload" && typeSpec.Name.Name != "AIProxyPayload" {
					continue
				}
			}

			if st, ok := typeSpec.Type.(*ast.StructType); ok {
				payloadStruct = st
				if targetStruct == "" && genDecl.Doc != nil {
					overallDoc = genDecl.Doc.Text()
				}
			}
		}
	}

	if payloadStruct == nil {
		// No target struct: emit the canonical no-input schema.
		if err := json.NewEncoder(os.Stdout).Encode(Output{Schema: noInputSchema()}); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write action metadata: %v\n", err)
			os.Exit(1)
		}
		return
	}

	parser := newSchemaParser(node)
	schema := parser.parseStruct(payloadStruct)

	out := Output{
		Description: strings.TrimSpace(overallDoc),
		Schema:      schema,
	}

	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write action metadata: %v\n", err)
		os.Exit(1)
	}
}

func noInputSchema() Schema {
	return Schema{
		Type:                 "object",
		Properties:           map[string]Schema{},
		AdditionalProperties: false,
		ForceProperties:      true,
	}
}

func inferPayloadStruct(file *ast.File) (string, string) {
	for _, decl := range file.Decls {
		fnDecl, ok := decl.(*ast.FuncDecl)
		if !ok || fnDecl.Body == nil {
			continue
		}

		structByVar := map[string]string{}
		resolvedStruct := ""
		resolvedDoc := ""

		ast.Inspect(fnDecl.Body, func(n ast.Node) bool {
			if resolvedStruct != "" {
				return false
			}

			switch node := n.(type) {
			case *ast.DeclStmt:
				genDecl, ok := node.Decl.(*ast.GenDecl)
				if !ok || genDecl.Tok != token.VAR {
					return true
				}

				for _, spec := range genDecl.Specs {
					valueSpec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}

					typeName := typeNameFromExpr(valueSpec.Type)
					if typeName == "" {
						continue
					}

					for _, name := range valueSpec.Names {
						structByVar[name.Name] = typeName
					}
				}

			case *ast.AssignStmt:
				if node.Tok != token.DEFINE {
					return true
				}

				for idx, lhs := range node.Lhs {
					lhsIdent, ok := lhs.(*ast.Ident)
					if !ok || idx >= len(node.Rhs) {
						continue
					}

					rhsType := typeNameFromCompositeLit(node.Rhs[idx])
					if rhsType != "" {
						structByVar[lhsIdent.Name] = rhsType
					}
				}

			case *ast.CallExpr:
				selector, ok := node.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Parse" || len(node.Args) != 1 {
					return true
				}

				varName := identNameFromParseArg(node.Args[0])
				if varName == "" {
					return true
				}

				structName, exists := structByVar[varName]
				if !exists {
					return true
				}

				resolvedStruct = structName
				if fnDecl.Doc != nil {
					resolvedDoc = strings.TrimSpace(removeMetadataAnnotationLines(fnDecl.Doc.Text()))
				}
				return false
			}

			return true
		})

		if resolvedStruct != "" {
			return resolvedStruct, resolvedDoc
		}
	}

	return "", ""
}

func typeNameFromExpr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeNameFromExpr(t.X)
	default:
		return ""
	}
}

func typeNameFromCompositeLit(expr ast.Expr) string {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return ""
	}

	return typeNameFromExpr(lit.Type)
}

func identNameFromParseArg(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.UnaryExpr:
		if t.Op == token.AND {
			if ident, ok := t.X.(*ast.Ident); ok {
				return ident.Name
			}
		}
	case *ast.Ident:
		return t.Name
	}

	return ""
}

func removeMetadataAnnotationLines(doc string) string {
	if doc == "" {
		return ""
	}

	lines := strings.Split(doc, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@Payload") {
			continue
		}
		filtered = append(filtered, line)
	}

	return strings.Join(filtered, "\n")
}

func newSchemaParser(file *ast.File) *schemaParser {
	parser := &schemaParser{
		typeSpecs: map[string]ast.Expr{},
		visiting:  map[string]bool{},
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}

		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			parser.typeSpecs[typeSpec.Name.Name] = typeSpec.Type
		}
	}

	return parser
}

func (p *schemaParser) parseStruct(st *ast.StructType) Schema {
	schema := Schema{
		Type:            "object",
		Properties:      make(map[string]Schema),
		Required:        []string{},
		ForceProperties: true,
	}

	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			continue
		}

		name := field.Names[0].Name

		var jsonschemaTags string

		if field.Tag != nil {
			tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
			jsonTag := tag.Get("json")
			if jsonTag != "" {
				parts := strings.Split(jsonTag, ",")
				if parts[0] == "-" {
					continue
				}
				if parts[0] != "" {
					name = parts[0]
				}
			}
			jsonschemaTags = tag.Get("jsonschema")
		}

		propSchema := p.parseType(field.Type)
		tags := parseJSONSchemaTags(jsonschemaTags)
		propSchema = applySchemaTags(propSchema, tags)

		if field.Doc != nil {
			propSchema.Description = strings.TrimSpace(field.Doc.Text())
		} else if field.Comment != nil {
			propSchema.Description = strings.TrimSpace(field.Comment.Text())
		}

		if tags.required {
			schema.Required = appendUnique(schema.Required, name)
		}

		schema.Properties[name] = propSchema
	}

	if len(schema.Required) == 0 {
		schema.Required = nil
	}

	return schema
}

func (p *schemaParser) parseType(expr ast.Expr) Schema {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return Schema{Type: "string"}
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
			return Schema{Type: "integer"}
		case "float32", "float64":
			return Schema{Type: "number"}
		case "bool":
			return Schema{Type: "boolean"}
		case "any":
			return Schema{Type: "object", AdditionalProperties: true}
		}

		if typeExpr, exists := p.typeSpecs[t.Name]; exists {
			if p.visiting[t.Name] {
				// Recursive type cycle guard
				return Schema{Type: "object"}
			}

			p.visiting[t.Name] = true
			resolved := p.parseType(typeExpr)
			delete(p.visiting, t.Name)
			return resolved
		}
	case *ast.ArrayType:
		itemSchema := p.parseType(t.Elt)
		return Schema{
			Type:  "array",
			Items: &itemSchema,
		}
	case *ast.MapType:
		// Go maps map string to X. This translates to an open object in JSON Schema.
		if isAnyTypeExpr(t.Value) {
			return Schema{
				Type:                 "object",
				AdditionalProperties: true,
			}
		}

		valueSchema := p.parseType(t.Value)

		return Schema{
			Type:                 "object",
			AdditionalProperties: valueSchema,
		}
	case *ast.SelectorExpr:
		if pkgIdent, ok := t.X.(*ast.Ident); ok && pkgIdent.Name == "json" && t.Sel.Name == "RawMessage" {
			return Schema{Type: "object", AdditionalProperties: true}
		}

		// Handling package.Type, for simple cases we assume object-like JSON.
		return Schema{Type: "object", AdditionalProperties: true}
	case *ast.StructType:
		return p.parseStruct(t)
	case *ast.StarExpr:
		return p.parseType(t.X)
	case *ast.InterfaceType:
		return Schema{Type: "object", AdditionalProperties: true}
	}

	// Default fallback
	return Schema{Type: "object", AdditionalProperties: true}
}

func appendUnique(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}

	return append(values, candidate)
}

func isAnyTypeExpr(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name == "any"
	case *ast.InterfaceType:
		return true
	case *ast.StarExpr:
		return isAnyTypeExpr(t.X)
	default:
		return false
	}
}

func parseJSONSchemaTags(raw string) parsedSchemaTags {
	tags := parsedSchemaTags{}
	if raw == "" {
		return tags
	}

	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		switch {
		case part == "required":
			tags.required = true
		case part == "nullable":
			tags.nullable = true
		case strings.HasPrefix(part, "default="):
			tags.defaultSet = true
			defaultRaw := strings.TrimPrefix(part, "default=")
			var parsed any
			if err := json.Unmarshal([]byte(defaultRaw), &parsed); err == nil {
				tags.defaultValue = parsed
			} else {
				tags.defaultValue = defaultRaw
			}
		case strings.HasPrefix(part, "enum="):
			tags.enumValues = splitTagList(strings.TrimPrefix(part, "enum="))
		case strings.HasPrefix(part, "type="):
			tags.typeOverride = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(part, "type=")))
		case strings.HasPrefix(part, "format="):
			tags.format = strings.TrimSpace(strings.TrimPrefix(part, "format="))
		case strings.HasPrefix(part, "minItems="):
			tags.minItems = parseTagInt(strings.TrimPrefix(part, "minItems="))
		case strings.HasPrefix(part, "maxItems="):
			tags.maxItems = parseTagInt(strings.TrimPrefix(part, "maxItems="))
		case strings.HasPrefix(part, "minLength="):
			tags.minLength = parseTagInt(strings.TrimPrefix(part, "minLength="))
		case strings.HasPrefix(part, "maxLength="):
			tags.maxLength = parseTagInt(strings.TrimPrefix(part, "maxLength="))
		case strings.HasPrefix(part, "pattern="):
			tags.pattern = strings.TrimPrefix(part, "pattern=")
		case strings.HasPrefix(part, "minimum="):
			tags.minimum = parseTagFloat(strings.TrimPrefix(part, "minimum="))
		case strings.HasPrefix(part, "maximum="):
			tags.maximum = parseTagFloat(strings.TrimPrefix(part, "maximum="))
		case strings.HasPrefix(part, "multipleOf="):
			tags.multipleOf = parseTagFloat(strings.TrimPrefix(part, "multipleOf="))
		case strings.HasPrefix(part, "additionalProperties="):
			value := strings.TrimSpace(strings.TrimPrefix(part, "additionalProperties="))
			if parsed, err := strconv.ParseBool(strings.ToLower(value)); err == nil {
				tags.additionalPropertiesSet = true
				tags.additionalProperties = parsed
			}
		case strings.HasPrefix(part, "items="):
			tags.itemsType = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(part, "items=")))
		case strings.HasPrefix(part, "anyOf="):
			tags.anyOfTypes = splitTagList(strings.TrimPrefix(part, "anyOf="))
			for idx, value := range tags.anyOfTypes {
				tags.anyOfTypes[idx] = strings.ToLower(value)
			}
		}
	}

	return tags
}

func splitTagList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}

	return out
}

func parseTagInt(raw string) *int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}

	return &parsed
}

func parseTagFloat(raw string) *float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}

	return &parsed
}

func applySchemaTags(schema Schema, tags parsedSchemaTags) Schema {
	if tags.typeOverride != "" {
		schema = schemaForTypeToken(tags.typeOverride)
	}

	if len(tags.anyOfTypes) > 0 {
		schema = Schema{AnyOf: schemasForTypeTokens(tags.anyOfTypes)}
	}

	if len(tags.enumValues) > 0 {
		enumValues := make([]any, 0, len(tags.enumValues))
		for _, value := range tags.enumValues {
			enumValues = append(enumValues, value)
		}
		schema.Enum = enumValues
	}

	if tags.format != "" {
		schema.Format = tags.format
	}

	if tags.minItems != nil {
		schema.MinItems = tags.minItems
	}
	if tags.maxItems != nil {
		schema.MaxItems = tags.maxItems
	}
	if tags.minLength != nil {
		schema.MinLength = tags.minLength
	}
	if tags.maxLength != nil {
		schema.MaxLength = tags.maxLength
	}
	if tags.pattern != "" {
		schema.Pattern = tags.pattern
	}
	if tags.minimum != nil {
		schema.Minimum = tags.minimum
	}
	if tags.maximum != nil {
		schema.Maximum = tags.maximum
	}
	if tags.multipleOf != nil {
		schema.MultipleOf = tags.multipleOf
	}

	if tags.additionalPropertiesSet {
		schema.AdditionalProperties = tags.additionalProperties
	}

	if tags.itemsType != "" {
		itemSchema := schemaForTypeToken(tags.itemsType)
		schema.Items = &itemSchema
		if schema.Type == "" && len(schema.AnyOf) == 0 {
			schema.Type = "array"
		}
	}

	if tags.defaultSet {
		schema.Default = tags.defaultValue
	}

	if tags.nullable {
		schema = makeNullable(schema)
	}

	return schema
}

func schemaForTypeToken(raw string) Schema {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "string":
		return Schema{Type: "string"}
	case "integer":
		return Schema{Type: "integer"}
	case "number":
		return Schema{Type: "number"}
	case "boolean":
		return Schema{Type: "boolean"}
	case "object":
		return Schema{Type: "object", AdditionalProperties: true}
	case "array":
		item := Schema{Type: "object", AdditionalProperties: true}
		return Schema{Type: "array", Items: &item}
	case "json":
		return jsonAnySchema(1)
	case "null":
		return Schema{Type: "null"}
	default:
		return Schema{Type: "object", AdditionalProperties: true}
	}
}

func schemasForTypeTokens(tokens []string) []Schema {
	out := make([]Schema, 0, len(tokens))
	for _, token := range tokens {
		if strings.EqualFold(token, "json") {
			out = append(out, jsonAnySchema(1).AnyOf...)
			continue
		}

		out = append(out, schemaForTypeToken(token))
	}

	return out
}

func jsonAnySchema(depth int) Schema {
	anyOf := []Schema{
		{Type: "string"},
		{Type: "number"},
		{Type: "integer"},
		{Type: "boolean"},
		{Type: "object", AdditionalProperties: true},
	}

	var items Schema
	if depth <= 0 {
		items = Schema{Type: "object", AdditionalProperties: true}
	} else {
		items = jsonAnySchema(depth - 1)
	}

	anyOf = append(anyOf, Schema{Type: "array", Items: &items})
	anyOf = append(anyOf, Schema{Type: "null"})

	return Schema{AnyOf: anyOf}
}

func makeNullable(schema Schema) Schema {
	if len(schema.AnyOf) > 0 {
		if !anyOfContainsNull(schema.AnyOf) {
			schema.AnyOf = append(schema.AnyOf, Schema{Type: "null"})
		}
		return schema
	}

	if schema.Type != "" {
		schema.AnyOf = []Schema{
			{Type: schema.Type},
			{Type: "null"},
		}
		schema.Type = ""
		return schema
	}

	schema.AnyOf = []Schema{{Type: "null"}}
	return schema
}

func anyOfContainsNull(schemas []Schema) bool {
	for _, schema := range schemas {
		if schema.Type == "null" {
			return true
		}
	}

	return false
}
