package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// THE AUTHOR-FACING EXPOSURE VOCABULARY.
//
// An action becomes callable by an agent because its own doc comment says so,
// one tag per line, in any doc block in the action's source. Carrying it
// in the source is what lets regeneration keep it: this generator rewrites
// action.json wholesale, so anything added to that file by hand is deleted the
// next time an author touches the action.
//
// Exposure is opt-in and there is no blocklist. An action that declares nothing
// is not a tool, so a new action is unreachable by an agent until its author
// writes the sentence that reaches it — rather than reachable until someone
// remembers to exclude it.
//
// The host, not the author, pins a tool's revision: it is not in this
// vocabulary and there is nothing here for an author to get wrong about it.
const (
	payloadAnnotation = "@Payload"

	aiTagPrefix = "@ai_"

	aiToolTag             = "ai_tool"
	aiEffectsTag          = "ai_effects"
	aiRetrySafetyTag      = "ai_retry_safety"
	aiDisclosureOriginTag = "ai_disclosure_origin"

	aiDefaultDisclosureOrigin = "tenant_record"
)

var (
	aiTags       = []string{aiToolTag, aiEffectsTag, aiRetrySafetyTag, aiDisclosureOriginTag}
	aiToolValues = []string{"true", "false"}

	aiEffects = []string{"read", "orchestration", "write", "destructive", "external", "credential"}

	aiRetrySafeties = []string{"safe", "idempotent_with_key", "verify_before_retry", "never_automatic"}

	aiDisclosureOrigins = []string{"tenant_record", "settings_field", "credential_field", "secret_field"}
)

type aiTag struct {
	name  string
	value string
}

type aiMetadata struct {
	Tool             bool     `json:"tool"`
	Effects          []string `json:"effects,omitempty"`
	RetrySafety      string   `json:"retry_safety,omitempty"`
	DisclosureOrigin string   `json:"disclosure_origin,omitempty"`
}

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
	Description string      `json:"description"`
	Schema      Schema      `json:"schema"`
	AI          *aiMetadata `json:"ai,omitempty"`
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

	// Every exposure annotation in the file, collected once, wherever its author
	// wrote it.
	//
	// Which doc block supplies the DESCRIPTION depends on how the payload is
	// declared, and that choice is made below. Reading annotations only from the
	// block that happened to win would drop a tag written in any of the others
	// in silence — and a dropped `@ai_tool` is an action that quietly stops being
	// callable, which is the failure this annotation exists to make impossible.
	// The same tag written twice is refused rather than resolved.
	tags := collectAITags(node)

	// Pass 1: Find a function with @Payload annotation
	for _, decl := range node.Decls {
		if fnDecl, ok := decl.(*ast.FuncDecl); ok {
			if fnDecl.Doc != nil {
				description, _, declaredStruct := splitDoc(fnDecl.Doc.Text())
				if declaredStruct != "" {
					targetStruct = declaredStruct
					overallDoc = description
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
					overallDoc, _, _ = splitDoc(genDecl.Doc.Text())
				}
			}
		}
	}

	// The exposure statement is settled before the schema is, and a malformed
	// one refuses here rather than downstream. An action whose payload could not
	// be resolved still gets its annotation read, so a typo is never masked by a
	// second, unrelated problem in the same file.
	//
	// A refusal exits with its own status, which is the only thing that survives
	// this process to say the SOURCE is wrong rather than that this program
	// could not run. The caller needs the difference: a refused source has made
	// the action.json already on disk describe an action that no longer exists,
	// and an absent Go toolchain has not.
	ai, err := buildAIMetadata(actionName(filePath), tags)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(annotationRefusalExitCode)
	}

	if payloadStruct == nil {
		// No target struct: emit the canonical no-input schema.
		if err := json.NewEncoder(os.Stdout).Encode(Output{Schema: noInputSchema(), AI: ai}); err != nil {
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
		AI:          ai,
	}

	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write action metadata: %v\n", err)
		os.Exit(1)
	}
}

// Every exposure annotation written in a file, in source order.
//
// Read from every documented declaration rather than only from the one the
// description came from, so where an author writes the statement does not
// decide whether it is heard.
func collectAITags(file *ast.File) []aiTag {
	var tags []aiTag

	for _, decl := range file.Decls {
		var doc *ast.CommentGroup

		switch typed := decl.(type) {
		case *ast.FuncDecl:
			doc = typed.Doc
		case *ast.GenDecl:
			doc = typed.Doc
		}

		if doc == nil {
			continue
		}

		_, docTags, _ := splitDoc(doc.Text())
		tags = append(tags, docTags...)
	}

	return tags
}

// The action a source file belongs to, so a refusal names the action its author
// is looking at rather than a path into its build layout.
func actionName(filePath string) string {
	name := filepath.Base(filepath.Dir(filePath))
	if name == "." || name == string(filepath.Separator) {
		return filePath
	}

	return name
}

// The description an action states about itself, the exposure annotations
// written beside it, and the payload struct its doc comment points at.
//
// The annotations are LIFTED OUT of the description rather than left in it. A
// tag left behind travels to the model as part of what the tool says it does —
// so every line the grammar claims is removed here, in the one place that knows
// the grammar, and the description is what remains.
func splitDoc(doc string) (string, []aiTag, string) {
	var descLines []string
	var tags []aiTag

	payloadStruct := ""

	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, payloadAnnotation):
			if parts := strings.Fields(trimmed); len(parts) >= 2 {
				payloadStruct = parts[1]
			}

		case strings.HasPrefix(trimmed, aiTagPrefix):
			name := strings.TrimPrefix(strings.Fields(trimmed)[0], "@")
			tags = append(tags, aiTag{
				name:  name,
				value: strings.TrimSpace(strings.TrimPrefix(trimmed, "@"+name)),
			})

		default:
			descLines = append(descLines, line)
		}
	}

	return strings.TrimSpace(strings.Join(descLines, "\n")), tags, payloadStruct
}

// The exposure statement an action makes about itself, or nothing at all.
//
// Absent means false: an action that writes no `@ai_` tag gets no `ai` object,
// which is how every action that is not a tool regenerates unchanged. Anything
// short of a complete, well-formed statement refuses instead of degrading,
// because a half-read annotation is how an action ends up advertised as
// something it is not.
func buildAIMetadata(action string, tags []aiTag) (*aiMetadata, error) {
	if len(tags) == 0 {
		return nil, nil
	}

	declared := map[string]string{}

	for _, tag := range tags {
		if !contains(aiTags, tag.name) {
			return nil, annotationError(action,
				fmt.Sprintf("@%s is not an exposure annotation", tag.name), prefixed(aiTags))
		}

		if _, seen := declared[tag.name]; seen {
			return nil, annotationError(action,
				fmt.Sprintf("@%s is declared more than once", tag.name), nil)
		}

		declared[tag.name] = tag.value
	}

	tool, stated := declared[aiToolTag]
	if !stated {
		return nil, annotationError(action,
			fmt.Sprintf("declares an exposure annotation without @%s, so it is not a tool and the rest says nothing", aiToolTag),
			aiToolValues)
	}

	if !contains(aiToolValues, tool) {
		return nil, annotationError(action, fmt.Sprintf("@%s takes %q", aiToolTag, tool), aiToolValues)
	}

	if tool == "false" {
		for _, tag := range []string{aiEffectsTag, aiRetrySafetyTag, aiDisclosureOriginTag} {
			if _, qualified := declared[tag]; qualified {
				return nil, annotationError(action,
					fmt.Sprintf("@%s qualifies a tool, and @%s is false", tag, aiToolTag), nil)
			}
		}

		return &aiMetadata{Tool: false}, nil
	}

	rawEffects, stated := declared[aiEffectsTag]
	if !stated {
		return nil, annotationError(action,
			fmt.Sprintf("is a tool and must declare @%s", aiEffectsTag), aiEffects)
	}

	effects, err := parseEffects(action, rawEffects)
	if err != nil {
		return nil, err
	}

	retrySafety, stated := declared[aiRetrySafetyTag]
	if !stated {
		return nil, annotationError(action,
			fmt.Sprintf("is a tool and must declare @%s", aiRetrySafetyTag), aiRetrySafeties)
	}

	if !contains(aiRetrySafeties, retrySafety) {
		return nil, annotationError(action,
			fmt.Sprintf("@%s takes %q", aiRetrySafetyTag, retrySafety), aiRetrySafeties)
	}

	disclosureOrigin, stated := declared[aiDisclosureOriginTag]
	if !stated {
		disclosureOrigin = aiDefaultDisclosureOrigin
	}

	if !contains(aiDisclosureOrigins, disclosureOrigin) {
		return nil, annotationError(action,
			fmt.Sprintf("@%s takes %q", aiDisclosureOriginTag, disclosureOrigin), aiDisclosureOrigins)
	}

	return &aiMetadata{
		Tool:             true,
		Effects:          effects,
		RetrySafety:      retrySafety,
		DisclosureOrigin: disclosureOrigin,
	}, nil
}

func parseEffects(action, raw string) ([]string, error) {
	effects := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})

	if len(effects) == 0 {
		return nil, annotationError(action, fmt.Sprintf("@%s names no effect", aiEffectsTag), aiEffects)
	}

	seen := map[string]bool{}

	for _, effect := range effects {
		if !contains(aiEffects, effect) {
			return nil, annotationError(action,
				fmt.Sprintf("@%s names an unknown effect %q", aiEffectsTag, effect), aiEffects)
		}

		if seen[effect] {
			return nil, annotationError(action,
				fmt.Sprintf("@%s names %q twice", aiEffectsTag, effect), aiEffects)
		}

		seen[effect] = true
	}

	return effects, nil
}

// The status a refused exposure statement exits with, told apart from every
// other way this program can fail so the caller can tell a wrong source from a
// missing toolchain.
const annotationRefusalExitCode = 2

func annotationError(action, message string, accepted []string) error {
	if len(accepted) == 0 {
		return fmt.Errorf("%s: %s", action, message)
	}

	return fmt.Errorf("%s: %s. Accepted: %s", action, message, strings.Join(accepted, ", "))
}

func prefixed(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, "@"+name)
	}

	return out
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}

	return false
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
					resolvedDoc, _, _ = splitDoc(fnDecl.Doc.Text())
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
