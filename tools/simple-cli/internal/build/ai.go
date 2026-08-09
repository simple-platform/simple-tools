package build

import (
	"regexp"
	"strings"
)

// THE AUTHOR-FACING EXPOSURE VOCABULARY, AS THIS TOOL NEEDS TO KNOW IT.
//
// An action becomes callable by an agent because its own doc comment says so,
// one tag per line, in any doc comment in the action's source. Carrying it in
// the source is what lets regeneration keep it: action.json is rewritten
// wholesale on every build, so anything added to that file by hand is deleted
// the next time an author touches the action.
//
// WHAT THE VOCABULARY MEANS IS NOT DECIDED HERE. The generator this package
// embeds reads it, enforces it, and refuses what it does not admit — in every
// language it describes, for both build entry points. This package used to
// enforce it a second time, in its own Go, and two readings of one contract is
// two contracts: they had already drifted over whether a payload field's comment
// counts, so the same source was refused by one tool and shipped by the other.
//
// What is left here is the NAMES, and only because a scaffolded space must
// declare them to TSDoc. A tag the editor has not been told about is reported as
// an unknown one, which teaches an author the annotation is a mistake at the
// moment they write the single line that makes their action reachable.
//
// `Payload` is one of them although it says nothing about exposure. The
// generator CLAIMS it — a line writing it is lifted out of the description
// rather than shipped to a model as prose — and the editor has to be told every
// name the build claims, or the one place the two disagree is a squiggle under a
// line the build was perfectly happy with.
var actionTags = []string{"tool", "shortdesc", "usewhen", "Payload"}

// ActionTagNames is the vocabulary the generator claims, in the order it is
// documented and without the leading `@`.
//
// It is exported so the tsdoc.json a space is scaffolded with can be held to the
// same names rather than restating them, and the list itself is held to the
// generator's own declarations, so this going stale is a failing test rather
// than a template that quietly stops declaring a tag.
func ActionTagNames() []string {
	return append([]string(nil), actionTags...)
}

// THE NAME AN ACTION'S PAYLOAD TYPE IS FOUND UNDER WHEN NOBODY POINTS AT ONE.
//
// A Rust action's input schema is read off a struct, and the struct is found by
// its name: `@Payload` names it where an author has one of their own, and this
// is what is looked for otherwise. It is the same name a TypeScript action
// declares its interface under, which is why one name serves both.
//
// IT IS READ FROM THE COMPANION RATHER THAN WRITTEN DOWN HERE. A type this name
// does not match is not found, and not being found is not an error — an action
// that takes no input is a real thing, so the companion answers with the
// no-input schema and says nothing. That is right for a source that has no
// payload and silent for one whose payload is simply called something else:
// what ships is an action advertising that it takes nothing, beside a handler
// that requires a field, and the first sign of it is a model calling the action
// with an empty object and being refused.
//
// The scaffolded Rust action is the one source this tool writes itself, so it
// is the one that must not fall into that hole. Holding it to the companion's
// own constant is what keeps the two in step: a rename upstream fails a test
// here rather than turning every newly scaffolded action into one that
// advertises no input.
var rustPayloadStructPattern = regexp.MustCompile(`(?m)^const DEFAULT_PAYLOAD_STRUCT: &str = "(\w+)";$`)

// PayloadStructName is the name the Rust companion reads an action's payload
// type under, as the companion itself declares it.
//
// It is exported so the scaffolded Rust action can be held to the name rather
// than restating it. Answers "" when the declaration cannot be found, which the
// caller is expected to fail on: a gate that cannot see the thing it checks has
// to say so, not pass.
func PayloadStructName() string {
	source, err := rustCompanionCrate.ReadFile(rustCompanionEmbedDir + "/src/main.rs")
	if err != nil {
		return ""
	}

	declaration := rustPayloadStructPattern.FindSubmatch(source)
	if declaration == nil {
		return ""
	}

	return string(declaration[1])
}

// THE GENERATOR NAMES ITS VOCABULARY, SO THIS READS THE NAME RATHER THAN
// COUNTING DECLARATIONS.
//
// It used to match every `const X_TAG = '...'` in the file and treat the result
// as the vocabulary. That held while the generator described one language. It
// stopped the moment it described more than one, because a tag belonging to
// another language's vocabulary would have been read as belonging to this one.
//
// It failed in the other direction too, and silently. The value pattern was
// `[a-z]+`, which cannot match an underscore, so a tag spelled with one was
// invisible to it — the check went on passing while the file it checks had
// gained two tags it could not see. A staleness gate that cannot see the change
// is the shape of bug this pair of checks exists to catch, so it is worth
// naming: it was blind to its own input.
//
// The same trap is why the value pattern below accepts a capital. `@Payload`
// names a type, so it is spelled the way a type is; a lower-case-only pattern
// would have read that declaration as no declaration and answered with the
// constant's own name, which matches nothing this package declares. That fails
// loudly rather than passing — but it fails saying the vocabulary drifted, which
// is not what would have happened.
//
// Reading the declared array fixes both. The array says which names are THIS
// vocabulary, and the constants say what those names are worth.
var (
	generatorVocabularyPattern = regexp.MustCompile(`(?m)^const ACTION_TAGS = \[([^\]]*)\]`)
	generatorTagValuePattern   = regexp.MustCompile(`(?m)^const ([A-Z_]+_TAG) = '([A-Za-z_]+)'$`)
)

// generatorActionTags is the vocabulary as the embedded generator declares it,
// in the order the generator lists it.
func generatorActionTags() []string {
	vocabulary := generatorVocabularyPattern.FindStringSubmatch(generatorScript)
	if vocabulary == nil {
		return nil
	}

	values := make(map[string]string)
	for _, declaration := range generatorTagValuePattern.FindAllStringSubmatch(generatorScript, -1) {
		values[declaration[1]] = declaration[2]
	}

	names := make([]string, 0, 4)

	for _, member := range strings.Split(vocabulary[1], ",") {
		member = strings.TrimSpace(member)
		if member == "" {
			continue
		}

		// A member the constants do not define is carried through as its own
		// name rather than dropped. Dropping it would shrink the vocabulary
		// quietly, which is the failure above wearing a different hat.
		if value, ok := values[member]; ok {
			names = append(names, value)
		} else {
			names = append(names, member)
		}
	}

	return names
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
// reassembled, because it is raised in a child process and a sentence that
// survives a process boundary intact is one both build entry points can be held
// to word for word.
type AnnotationRefusal struct {
	Refusal string
}

func (r *AnnotationRefusal) Error() string {
	return r.Refusal
}

// AnnotationRefusalExitCode is what the generator exits with when it refuses an
// exposure statement, as opposed to failing to run at all. It is the only way
// the difference survives a child process, and the difference decides whether
// the stale action.json is discarded with the refusal.
const AnnotationRefusalExitCode = 2
