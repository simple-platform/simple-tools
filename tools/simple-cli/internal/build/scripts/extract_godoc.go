package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/doc"
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
// An action becomes callable by an agent because its own source says so, one
// tag per line, anywhere in a comment in the action's main file, once. Carrying
// it in the source is what lets regeneration keep it: this generator rewrites
// action.json wholesale, so anything added to that file by hand is deleted the
// next time an author touches the action.
//
// Exposure is opt-in and there is no blocklist. An action that declares nothing
// is not a tool, so a new action is unreachable by an agent until its author
// writes the sentence that reaches it — rather than reachable until someone
// remembers to exclude it.
//
// `@tool` IS A MODIFIER TAG: it carries no value, and its presence is the whole
// statement. A tag that took a boolean made absence mean a default — an action
// was not a tool because nobody said `false` — and a default is a claim about
// actions nobody has read. Presence-only says the narrower true thing: an action
// is unmarked until its author marks it, the way a symbol is not `@public` until
// it says so.
//
// Only these four names are claimed as annotations. Every other `@` line is
// description, because this vocabulary shares a doc comment with `@Payload` here
// and with `@param`, `@remarks` and the rest of TSDoc on the other generator,
// and one that lifted every tag out of the description would delete an author's
// prose to protect its own.
//
// A NAME ONE EDIT AWAY FROM A CLAIMED ONE IS REFUSED RATHER THAN LEFT AS PROSE.
// Nothing else can catch it here: a Go doc comment has no editor lint behind it,
// so `@dicloses secret_field` was read as a sentence, the class it was written to
// tighten fell back to the loosest one, and the line itself travelled into the
// description a model reads. Both halves of that are silent, and the tag an
// author most wants heard is the one that says what calling the tool discloses.
//
// The host, not the author, pins a tool's revision: it is not in this
// vocabulary and there is nothing here for an author to get wrong about it.
const (
	payloadAnnotation = "@Payload"

	toolTag      = "tool"
	effectsTag   = "effects"
	retryTag     = "retry"
	disclosesTag = "discloses"

	defaultDiscloses = "tenant_record"
)

var (
	exposureTags = []string{toolTag, effectsTag, retryTag, disclosesTag}

	// The tags that say what CALLING a tool does. Each one qualifies `@tool`, so
	// any of them written without it is a statement about nothing.
	qualifyingTags = []string{effectsTag, retryTag, disclosesTag}

	effectValues = []string{"read", "orchestration", "write", "destructive", "external", "credential"}

	retryValues = []string{"safe", "keyed", "verify-first", "never"}

	disclosesValues = []string{"tenant_record", "settings_field", "credential_field", "secret_field"}
)

type exposureTag struct {
	name  string
	value string
}

// A tag one edit from a claimed name: what the author wrote, and the name it is
// one edit from.
type misspelledTag struct {
	written string
	meant   string
}

// What one doc comment says: the prose it states, the annotations claimed out of
// it, the payload struct it names, and any tag written one edit from a claimed
// name.
type docContent struct {
	description   string
	tags          []exposureTag
	payloadStruct string
	misspelled    []misspelledTag
}

// The action's exposure statement as action.json carries it. Tool is always true
// where this object exists at all — it is how the artifact renders the presence
// of a modifier tag, so a reader of action.json alone sees the same statement
// the source makes.
type aiMetadata struct {
	Tool      bool     `json:"tool"`
	Effects   []string `json:"effects,omitempty"`
	Retry     string   `json:"retry,omitempty"`
	Discloses string   `json:"discloses,omitempty"`
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

	// WHICH COMMENT DOCUMENTS WHICH DECLARATION IS GO'S QUESTION, AND GO ANSWERS
	// IT.
	//
	// This program used to decide, by walking the file's declarations and reading
	// the doc hanging off each one. That walk knew about two shapes and the
	// language has more: a comment above `package` documented the package and was
	// read by nothing, and a declaration written inside a `type (...)` or
	// `const (...)` group carries its doc on the spec rather than on the group, so
	// that was read by nothing either.
	//
	// `AllDecls` because an action's handler is unexported and its payload need
	// not be; `PreserveAST` because the same tree is read again below for the
	// schema, and go/doc otherwise edits what it was handed.
	pkg, err := doc.NewFromFiles(fset, []*ast.File{node}, "action", doc.AllDecls|doc.PreserveAST)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Doc error: %v\n", err)
		os.Exit(1)
	}

	// Every exposure annotation in the file, collected once, wherever its author
	// wrote it.
	//
	// Which comment supplies the DESCRIPTION is decided just below, by where the
	// payload is declared. Reading annotations only from the comment that
	// happened to win would drop a tag written in any of the others in silence —
	// and a dropped `@tool` is an action that quietly stops being callable, which
	// is the failure this annotation exists to make impossible. The same tag
	// written twice is refused rather than resolved.
	stated := collectExposureTags(node)

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
	ai, err := buildAIMetadata(actionName(filePath), stated)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(annotationRefusalExitCode)
	}

	schemas := newSchemaParser(node)
	targetStruct, overallDoc := describedPayload(pkg)
	payloadStruct := schemas.structNamed(targetStruct)

	if payloadStruct == nil {
		// No target struct: emit the canonical no-input schema.
		if err := json.NewEncoder(os.Stdout).Encode(Output{Schema: noInputSchema(), AI: ai}); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write action metadata: %v\n", err)
			os.Exit(1)
		}

		return
	}

	out := Output{
		Description: strings.TrimSpace(overallDoc),
		Schema:      schemas.parseStruct(payloadStruct),
		AI:          ai,
	}

	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write action metadata: %v\n", err)
		os.Exit(1)
	}
}

// The struct an action's payload is declared as, and the description the action
// states about itself.
//
// Both are read from the doc Go itself attributes to a declaration, which is
// what an editor shows and what `go doc` prints. The rule that produced them
// here knew about a doc comment written on a `type` declaration and not about
// one written on a spec inside a `type (...)` group, so an author who grouped
// their payload with anything else shipped an action with no description and
// nothing said.
//
// The order is what an author most specifically wrote: the function that names
// the payload, then the function that parses one without saying so, then a
// conventionally named payload type describing itself. A package's own doc is
// not in that order — it describes the file, and an action that says nothing
// about itself is better left undescribed than described by the wrong sentence.
func describedPayload(pkg *doc.Package) (string, string) {
	for _, function := range pkg.Funcs {
		if content := splitDoc(function.Doc); content.payloadStruct != "" {
			return content.payloadStruct, content.description
		}
	}

	// No `@Payload`: the struct the handler parses into says the same thing
	// without saying it, so schema generation survives a doc comment that omits
	// the annotation.
	for _, function := range pkg.Funcs {
		if inferred := parsedPayloadStruct(function.Decl); inferred != "" {
			return inferred, splitDoc(function.Doc).description
		}
	}

	for _, name := range []string{"Input", "Payload", "AIProxyPayload"} {
		for _, declared := range pkg.Types {
			if declared.Name == name {
				return name, splitDoc(declared.Doc).description
			}
		}
	}

	return "", ""
}

// Every exposure annotation written in a file, and every tag written one edit
// from one, collected once each wherever their author wrote them.
//
// EVERY COMMENT IN THE FILE IS READ, whatever it is or is not attached to. The
// parser hands back the whole set, and that is the whole rule: a tag is heard
// because it was written, not because of where.
//
// Reading only the comments attached to a declaration is what was here, and
// attachment is not something an author controls or sees. A blank line between
// a statement and the declaration under it detaches the comment, so the same
// four lines exposed the action or did not depending on an empty line nobody
// would think to look at — and the build stayed green either way, because an
// unheard `@tool` is indistinguishable from an action that never claimed to be
// a tool.
func collectExposureTags(file *ast.File) docContent {
	var stated docContent

	for _, group := range file.Comments {
		content := splitDoc(group.Text())
		stated.tags = append(stated.tags, content.tags...)
		stated.misspelled = append(stated.misspelled, content.misspelled...)
	}

	return stated
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
// the grammar, and the description is what remains. A line naming any other tag
// is left alone; it belongs to whoever else reads this doc comment.
func splitDoc(text string) docContent {
	var content docContent
	var descLines []string

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, payloadAnnotation) {
			if parts := strings.Fields(trimmed); len(parts) >= 2 {
				content.payloadStruct = parts[1]
			}

			continue
		}

		if tag, annotated := exposureTagFromDocLine(trimmed); annotated {
			content.tags = append(content.tags, tag)
			continue
		}

		// A near miss is left in the description rather than lifted out of it,
		// because it is refused before any description ships.
		if near, mistyped := misspelledTagFromDocLine(trimmed); mistyped {
			content.misspelled = append(content.misspelled, near)
		}

		descLines = append(descLines, line)
	}

	content.description = strings.TrimSpace(strings.Join(descLines, "\n"))

	return content
}

// One doc-comment line read as an exposure annotation, or left to the
// description.
func exposureTagFromDocLine(line string) (exposureTag, bool) {
	name, tagged := docLineTagName(line)
	if !tagged || !contains(exposureTags, name) {
		return exposureTag{}, false
	}

	return exposureTag{
		name:  name,
		value: strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "@"+name)),
	}, true
}

// One doc-comment line read as a claimed name its author mistyped.
//
// Only a name NOTHING claims is a candidate: `@Payload` is this generator's, and
// a tag some other reader of the comment claims is that reader's business. What
// is left is a line an author wrote as an annotation that no reader will ever
// hear, and the whole point of the vocabulary is that such a line cannot pass
// silently.
func misspelledTagFromDocLine(line string) (misspelledTag, bool) {
	name, tagged := docLineTagName(line)
	if !tagged || name == strings.TrimPrefix(payloadAnnotation, "@") || contains(exposureTags, name) {
		return misspelledTag{}, false
	}

	for _, claimed := range exposureTags {
		if withinOneEdit(name, claimed) {
			return misspelledTag{written: name, meant: claimed}, true
		}
	}

	return misspelledTag{}, false
}

// The tag name a doc-comment line begins with, if it begins with one at all.
func docLineTagName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "@") {
		return "", false
	}

	return strings.TrimPrefix(strings.Fields(trimmed)[0], "@"), true
}

// Whether one name reaches the other by inserting, deleting or replacing a
// single character.
//
// One edit is the distance a typo travels. Any more and a name stops being a
// misspelling of this vocabulary and starts being somebody else's word, which
// this generator has no business refusing.
func withinOneEdit(written, claimed string) bool {
	if written == claimed {
		return true
	}

	longer, shorter := written, claimed
	if len(shorter) > len(longer) {
		longer, shorter = shorter, longer
	}

	if len(longer)-len(shorter) > 1 {
		return false
	}

	for index := 0; index < len(shorter); index++ {
		if longer[index] == shorter[index] {
			continue
		}

		if len(longer) == len(shorter) {
			// A replacement: the rest must match exactly.
			return longer[index+1:] == shorter[index+1:]
		}

		// An insertion in the longer name: the rest must match what is left of
		// the shorter one.
		return longer[index+1:] == shorter[index:]
	}

	// Every character matched up to the shorter name's end, so the longer name
	// is at most one character further on.
	return true
}

// The exposure statement an action makes about itself, or nothing at all.
//
// An action that writes no exposure tag gets no `ai` object, which is how every
// action that is not a tool regenerates unchanged. Anything short of a complete,
// well-formed statement refuses instead of degrading, because a half-read
// annotation is how an action ends up advertised as something it is not.
func buildAIMetadata(action string, statement docContent) (*aiMetadata, error) {
	// A MISTYPED NAME IS REFUSED BEFORE ANYTHING ELSE IS READ.
	//
	// It is checked ahead of the tags because it explains them: an action
	// missing the tag it looks like it declares is missing it BECAUSE of this
	// line, and a refusal naming the incomplete statement would send its author
	// to add a tag they have already written.
	//
	// Refused even where the action declares nothing else, which is the case
	// that shipped. A lone mistyped `@discloses` left the action carrying the
	// loosest class by default and the line itself in the description, and
	// nothing anywhere said so.
	// The first one written, so fixing it and running again surfaces the next
	// rather than a list an author has to work through in one pass.
	if len(statement.misspelled) > 0 {
		near := statement.misspelled[0]

		return nil, annotationError(action,
			fmt.Sprintf("writes @%s, which nothing claims and which is one edit from @%s",
				near.written, near.meant), prefixed(exposureTags))
	}

	if len(statement.tags) == 0 {
		return nil, nil
	}

	declared := map[string]string{}

	for _, tag := range statement.tags {
		if _, seen := declared[tag.name]; seen {
			return nil, annotationError(action,
				fmt.Sprintf("@%s is declared more than once", tag.name), nil)
		}

		declared[tag.name] = tag.value
	}

	value, marked := declared[toolTag]
	if !marked {
		// Named in vocabulary order rather than in the order they were declared,
		// so the same source is refused with the same sentence every time.
		var written []string

		for _, tag := range qualifyingTags {
			if _, qualified := declared[tag]; qualified {
				written = append(written, tag)
			}
		}

		return nil, annotationError(action,
			fmt.Sprintf("declares %s without @%s, so it is not a tool and the rest says nothing",
				strings.Join(prefixed(written), ", "), toolTag), nil)
	}

	// A modifier tag is its own statement. A value written after one is an
	// author saying something the vocabulary has no way to hear — most likely
	// the boolean this tag used to take, whose `false` no longer says anything.
	if value != "" {
		return nil, annotationError(action,
			fmt.Sprintf("@%s is a modifier tag and takes no value, and this one carries %q. Leave it bare to expose the action, or delete it to leave the action unexposed",
				toolTag, value), nil)
	}

	rawEffects, stated := declared[effectsTag]
	if !stated {
		return nil, annotationError(action,
			fmt.Sprintf("is a tool and must declare @%s", effectsTag), effectValues)
	}

	effects, err := parseEffects(action, rawEffects)
	if err != nil {
		return nil, err
	}

	retry, stated := declared[retryTag]
	if !stated {
		return nil, annotationError(action,
			fmt.Sprintf("is a tool and must declare @%s", retryTag), retryValues)
	}

	if !contains(retryValues, retry) {
		return nil, annotationError(action,
			fmt.Sprintf("@%s takes %q", retryTag, retry), retryValues)
	}

	discloses, stated := declared[disclosesTag]
	if !stated {
		discloses = defaultDiscloses
	}

	if !contains(disclosesValues, discloses) {
		return nil, annotationError(action,
			fmt.Sprintf("@%s takes %q", disclosesTag, discloses), disclosesValues)
	}

	return &aiMetadata{
		Tool:      true,
		Effects:   effects,
		Retry:     retry,
		Discloses: discloses,
	}, nil
}

func parseEffects(action, raw string) ([]string, error) {
	effects := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})

	if len(effects) == 0 {
		return nil, annotationError(action, fmt.Sprintf("@%s names no effect", effectsTag), effectValues)
	}

	seen := map[string]bool{}

	for _, effect := range effects {
		if !contains(effectValues, effect) {
			return nil, annotationError(action,
				fmt.Sprintf("@%s names an unknown effect %q", effectsTag, effect), effectValues)
		}

		if seen[effect] {
			return nil, annotationError(action,
				fmt.Sprintf("@%s names %q twice", effectsTag, effect), effectValues)
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

// The struct a function parses its request into, for an action whose doc
// comment never says.
func parsedPayloadStruct(fnDecl *ast.FuncDecl) string {
	if fnDecl == nil || fnDecl.Body == nil {
		return ""
	}

	structByVar := map[string]string{}
	resolvedStruct := ""

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

			return false
		}

		return true
	})

	return resolvedStruct
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

// The struct a name is declared as, wherever in the file it is declared —
// including inside a `type (...)` group, which the search that read the file's
// declarations one by one could not see.
func (p *schemaParser) structNamed(name string) *ast.StructType {
	if name == "" {
		return nil
	}

	declared, exists := p.typeSpecs[name]
	if !exists {
		return nil
	}

	structType, isStruct := declared.(*ast.StructType)
	if !isStruct {
		return nil
	}

	return structType
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

		// Split like every other description, rather than trimmed and kept whole.
		// The grammar's lines are removed in the one place that knows the
		// grammar, and a description that skipped it shipped `@effects
		// destructive` to a model as a sentence about what the field means.
		if field.Doc != nil {
			propSchema.Description = splitDoc(field.Doc.Text()).description
		} else if field.Comment != nil {
			propSchema.Description = splitDoc(field.Comment.Text()).description
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
