package build

import "regexp"

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

// generatorTagPattern finds the tag names the embedded generator claims, one per
// declaration, so the list above can be checked against the file that enforces
// it rather than against a memory of it.
var generatorTagPattern = regexp.MustCompile(`(?m)^const [A-Z_]+_TAG = '([a-z]+)'$`)

// generatorExposureTags is the vocabulary as the embedded generator declares it.
func generatorExposureTags() []string {
	matches := generatorTagPattern.FindAllStringSubmatch(extractScriptContent, -1)

	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match[1])
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
