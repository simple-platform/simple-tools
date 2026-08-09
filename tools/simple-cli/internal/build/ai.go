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
// embeds reads it, enforces it, and refuses what it does not admit — in both
// languages, for both build entry points. This package used to enforce it a
// second time, in its own Go, and two readings of one contract is two
// contracts: they had already drifted over whether a payload field's comment
// counts, so the same source was refused by one tool and shipped by the other.
//
// What is left here is the NAMES, and only because a scaffolded space must
// declare them to TSDoc. A tag the editor has not been told about is reported as
// an unknown one, which teaches an author the annotation is a mistake at the
// moment they write the single line that makes their action reachable.
var exposureTags = []string{"tool", "effects", "retry", "discloses"}

// ExposureTagNames is the vocabulary the generator enforces, in the order it is
// documented and without the leading `@`.
//
// It is exported so the tsdoc.json a space is scaffolded with can be held to the
// same names rather than restating them, and the list itself is held to the
// generator's own declarations, so this going stale is a failing test rather
// than a template that quietly stops declaring a tag.
func ExposureTagNames() []string {
	return append([]string(nil), exposureTags...)
}

// THE GENERATOR NAMES ITS VOCABULARY, SO THIS READS THE NAME RATHER THAN
// COUNTING DECLARATIONS.
//
// It used to match every `const X_TAG = '...'` in the file and treat the result
// as the vocabulary. That held while the generator described one language. It
// stopped the moment it described two: the script now declares a Rust vocabulary
// beside this one, and a tag belonging to that vocabulary would have been read
// as belonging to this one.
//
// It failed in the other direction too, and silently. The value pattern was
// `[a-z]+`, which cannot match an underscore, so `short_desc` and `when_use`
// were invisible to it — the check went on passing while the file it checks had
// gained two tags it could not see. A staleness gate that cannot see the change
// is the shape of bug this pair of checks exists to catch, so it is worth
// naming: it was blind to its own input.
//
// Reading the declared array fixes both. The array says which names are THIS
// vocabulary, and the constants say what those names are worth.
var (
	generatorVocabularyPattern = regexp.MustCompile(`(?m)^const EXPOSURE_TAGS = \[([^\]]*)\]`)
	generatorTagValuePattern   = regexp.MustCompile(`(?m)^const ([A-Z_]+_TAG) = '([a-z_]+)'$`)
)

// generatorExposureTags is the vocabulary as the embedded generator declares it,
// in the order the generator lists it.
func generatorExposureTags() []string {
	vocabulary := generatorVocabularyPattern.FindStringSubmatch(extractScriptContent)
	if vocabulary == nil {
		return nil
	}

	values := make(map[string]string)
	for _, declaration := range generatorTagValuePattern.FindAllStringSubmatch(extractScriptContent, -1) {
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
