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

// AIMetadata is an action's statement about whether an agent may call it, and
// what calling it can do. It is written beside the description and the schema in
// action.json, and omitted entirely for an action that declares nothing.
type AIMetadata struct {
	Tool             bool     `json:"tool"`
	Effects          []string `json:"effects,omitempty"`
	RetrySafety      string   `json:"retry_safety,omitempty"`
	DisclosureOrigin string   `json:"disclosure_origin,omitempty"`
}

type aiTag struct {
	name  string
	value string
}

// aiTagFromDocLine reads one doc-comment line as an exposure annotation.
//
// The caller keeps the lines this does NOT claim, so a tag can never travel to
// the model as part of what the tool says it does: the description is what is
// left after every annotation line has been lifted out of it.
func aiTagFromDocLine(line string) (aiTag, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, aiTagPrefix) {
		return aiTag{}, false
	}

	name := strings.TrimPrefix(strings.Fields(trimmed)[0], "@")

	return aiTag{
		name:  name,
		value: strings.TrimSpace(strings.TrimPrefix(trimmed, "@"+name)),
	}, true
}

// collectAITags is every exposure annotation written in a file, in source order.
//
// Read from every documented declaration rather than only from the one the
// description came from, so where an author writes the statement does not decide
// whether it is heard. The same tag written twice is refused rather than
// resolved.
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

		for _, comment := range doc.List {
			text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
			if tag, annotated := aiTagFromDocLine(text); annotated {
				tags = append(tags, tag)
			}
		}
	}

	return tags
}

// buildAIMetadata is the exposure statement an action makes about itself, or
// nothing at all.
//
// Absent means false: an action that writes no `@ai_` tag gets no metadata,
// which is how every action that is not a tool builds unchanged. Anything short
// of a complete, well-formed statement refuses instead of degrading, because a
// half-read annotation is how an action ends up advertised as something it is
// not.
func buildAIMetadata(action string, tags []aiTag) (*AIMetadata, error) {
	if len(tags) == 0 {
		return nil, nil
	}

	declared := map[string]string{}

	for _, tag := range tags {
		if !containsString(aiTags, tag.name) {
			return nil, annotationError(action,
				fmt.Sprintf("@%s is not an exposure annotation", tag.name), prefixedTags(aiTags))
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

	if !containsString(aiToolValues, tool) {
		return nil, annotationError(action, fmt.Sprintf("@%s takes %q", aiToolTag, tool), aiToolValues)
	}

	if tool == "false" {
		for _, tag := range []string{aiEffectsTag, aiRetrySafetyTag, aiDisclosureOriginTag} {
			if _, qualified := declared[tag]; qualified {
				return nil, annotationError(action,
					fmt.Sprintf("@%s qualifies a tool, and @%s is false", tag, aiToolTag), nil)
			}
		}

		return &AIMetadata{Tool: false}, nil
	}

	rawEffects, stated := declared[aiEffectsTag]
	if !stated {
		return nil, annotationError(action,
			fmt.Sprintf("is a tool and must declare @%s", aiEffectsTag), aiEffects)
	}

	effects, err := parseAIEffects(action, rawEffects)
	if err != nil {
		return nil, err
	}

	retrySafety, stated := declared[aiRetrySafetyTag]
	if !stated {
		return nil, annotationError(action,
			fmt.Sprintf("is a tool and must declare @%s", aiRetrySafetyTag), aiRetrySafeties)
	}

	if !containsString(aiRetrySafeties, retrySafety) {
		return nil, annotationError(action,
			fmt.Sprintf("@%s takes %q", aiRetrySafetyTag, retrySafety), aiRetrySafeties)
	}

	disclosureOrigin, stated := declared[aiDisclosureOriginTag]
	if !stated {
		disclosureOrigin = aiDefaultDisclosureOrigin
	}

	if !containsString(aiDisclosureOrigins, disclosureOrigin) {
		return nil, annotationError(action,
			fmt.Sprintf("@%s takes %q", aiDisclosureOriginTag, disclosureOrigin), aiDisclosureOrigins)
	}

	return &AIMetadata{
		Tool:             true,
		Effects:          effects,
		RetrySafety:      retrySafety,
		DisclosureOrigin: disclosureOrigin,
	}, nil
}

func parseAIEffects(action, raw string) ([]string, error) {
	effects := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})

	if len(effects) == 0 {
		return nil, annotationError(action, fmt.Sprintf("@%s names no effect", aiEffectsTag), aiEffects)
	}

	seen := map[string]bool{}

	for _, effect := range effects {
		if !containsString(aiEffects, effect) {
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
