package build

import (
	"fmt"
	"go/ast"
	"strings"
)

// THE AUTHOR-FACING EXPOSURE VOCABULARY.
//
// An action becomes callable by an agent because its own doc comment says so,
// one tag per line, in any doc block in the action's source. Carrying it
// in the source is what lets regeneration keep it: action.json is rewritten
// wholesale on every build, so anything added to that file by hand is deleted
// the next time an author touches the action.
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
// description, because this vocabulary shares a doc comment with `@param`,
// `@remarks` and the rest of TSDoc, and a generator that lifted every tag out of
// the description would delete an author's prose to protect its own. What
// catches a misspelling is tsdoc.json, which declares these four to the editor
// and to ESLint, so `@toool` is reported where it is typed rather than silently
// read as a sentence.
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

// AIMetadata is an action's statement about whether an agent may call it, and
// what calling it can do. It is written beside the description and the schema in
// action.json, and omitted entirely for an action that declares nothing.
//
// Tool is always true where this object exists at all — it is how the artifact
// renders the presence of a modifier tag, so a reader of action.json alone sees
// the same statement the source makes.
type AIMetadata struct {
	Tool      bool     `json:"tool"`
	Effects   []string `json:"effects,omitempty"`
	Retry     string   `json:"retry,omitempty"`
	Discloses string   `json:"discloses,omitempty"`
}

// ExposureTagNames is the vocabulary these generators enforce, in the order it
// is documented and without the leading `@`.
//
// It is exported so the tsdoc.json a space is scaffolded with can be held to the
// same names rather than restating them. A tag the editor has not been told
// about is reported as an unknown one, which teaches an author that the
// annotation is a mistake at the moment they write the single line that makes
// their action reachable — so the declared set going stale is not cosmetic.
func ExposureTagNames() []string {
	return append([]string(nil), exposureTags...)
}

type exposureTag struct {
	name  string
	value string
}

// exposureTagFromDocLine reads one doc-comment line as an exposure annotation.
//
// The caller keeps the lines this does NOT claim, so a tag can never travel to
// the model as part of what the tool says it does: the description is what is
// left after every annotation line has been lifted out of it. A line naming any
// other tag is left alone — it belongs to whoever else reads this doc comment.
func exposureTagFromDocLine(line string) (exposureTag, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "@") {
		return exposureTag{}, false
	}

	name := strings.TrimPrefix(strings.Fields(trimmed)[0], "@")
	if !containsString(exposureTags, name) {
		return exposureTag{}, false
	}

	return exposureTag{
		name:  name,
		value: strings.TrimSpace(strings.TrimPrefix(trimmed, "@"+name)),
	}, true
}

// collectExposureTags is every exposure annotation written in a file, in source
// order.
//
// Read from every documented declaration rather than only from the one the
// description came from, so where an author writes the statement does not decide
// whether it is heard. The same tag written twice is refused rather than
// resolved.
func collectExposureTags(file *ast.File) []exposureTag {
	var tags []exposureTag

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

		for _, comment := range doc.List {
			text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
			if tag, annotated := exposureTagFromDocLine(text); annotated {
				tags = append(tags, tag)
			}
		}
	}

	return tags
}

// buildAIMetadata is the exposure statement an action makes about itself, or
// nothing at all.
//
// An action that writes no exposure tag gets no metadata, which is how every
// action that is not a tool builds unchanged. Anything short of a complete,
// well-formed statement refuses instead of degrading, because a half-read
// annotation is how an action ends up advertised as something it is not.
func buildAIMetadata(action string, tags []exposureTag) (*AIMetadata, error) {
	if len(tags) == 0 {
		return nil, nil
	}

	declared := map[string]string{}

	for _, tag := range tags {
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
				strings.Join(prefixedTags(written), ", "), toolTag), nil)
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

	if !containsString(retryValues, retry) {
		return nil, annotationError(action,
			fmt.Sprintf("@%s takes %q", retryTag, retry), retryValues)
	}

	discloses, stated := declared[disclosesTag]
	if !stated {
		discloses = defaultDiscloses
	}

	if !containsString(disclosesValues, discloses) {
		return nil, annotationError(action,
			fmt.Sprintf("@%s takes %q", disclosesTag, discloses), disclosesValues)
	}

	return &AIMetadata{
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
		if !containsString(effectValues, effect) {
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

// AnnotationRefusal is an exposure statement the vocabulary does not admit.
//
// It is a DISTINCT KIND OF FAILURE from a generator that could not run, and the
// distinction decides what happens to the action.json already on disk. A
// toolchain that is absent says nothing about the file: it may still describe
// the source faithfully, and it will as soon as the toolchain is back. A source
// the generator refuses says the file describes an action that no longer
// exists — so it is discarded rather than left to be read as current by
// everything downstream that cannot tell.
//
// Both fail the build. Only this one takes the stale file with it.
//
// It carries the whole author-facing sentence rather than parts to be
// reassembled, because the same refusal is raised by the Go extractor in this
// package and by the Node one in a child process, and a sentence that survives
// a process boundary intact is one both can be held to word for word.
type AnnotationRefusal struct {
	Refusal string
}

func (r *AnnotationRefusal) Error() string {
	return r.Refusal
}

// AnnotationRefusalExitCode is what the Node generator exits with when it
// refuses an exposure statement, as opposed to failing to run at all. It is the
// only way the difference survives a child process, and the difference decides
// whether the stale action.json is discarded with the refusal.
const AnnotationRefusalExitCode = 2

// annotationError names the action, the tag, and what would have been accepted.
// An author reading it should not have to open the generator to learn what to
// write instead.
func annotationError(action, message string, accepted []string) error {
	if len(accepted) > 0 {
		message = fmt.Sprintf("%s. Accepted: %s", message, strings.Join(accepted, ", "))
	}

	return &AnnotationRefusal{Refusal: fmt.Sprintf("%s: %s", action, message)}
}

func prefixedTags(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, "@"+name)
	}

	return out
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}

	return false
}
