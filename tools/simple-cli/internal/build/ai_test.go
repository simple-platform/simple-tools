package build

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// THE NAMES THIS PACKAGE DECLARES ARE THE NAMES THE GENERATOR ENFORCES.
//
// This package no longer reads the vocabulary — the generator it embeds does —
// and all that is left here is the list a scaffolded space declares to TSDoc.
// A list nothing checks is a list that goes stale silently: the editor stops
// knowing about a tag the build enforces, and an author is told their annotation
// is an unknown one at the moment they write the line that makes their action
// reachable.
//
// The generator is embedded in this binary, so the check costs nothing: the
// declarations are right there.
func TestTheDeclaredVocabularyIsTheGeneratorsOwn(t *testing.T) {
	declared := generatorActionTags()

	if len(declared) == 0 {
		t.Fatal("no tag declarations were found in the embedded generator, so this check proves nothing")
	}

	if strings.Join(declared, ",") != strings.Join(ActionTagNames(), ",") {
		t.Fatalf("the generator claims %v and this package declares %v", declared, ActionTagNames())
	}
}

// THE GO EXTRACTOR NAMES ITS VOCABULARY TOO, AND IS READ THE SAME WAY.
//
// Symmetry with the script side is the point, not tidiness. This used to match
// every `xTag = "..."` const in the file and call the result the vocabulary,
// which is true only while the file declares exactly one — and it holds today by
// luck rather than by construction: the value pattern was `[a-z]+`, so a tag
// spelled with an underscore was invisible to it, and the script side had
// already gained two of those without this check noticing. Reading the declared
// array means a second vocabulary appearing here is read as a second
// vocabulary rather than folded into this one.
var (
	goExtractorVocabularyPattern = regexp.MustCompile(`(?m)^\texposureTags = \[\]string\{([^}]*)\}`)
	goExtractorTagValuePattern   = regexp.MustCompile(`(?m)^\t(\w+Tag)\s+= "([a-z_]+)"$`)
)

// goExtractorExposureTags is the vocabulary as the embedded Go extractor
// declares it, in the order it lists it.
func goExtractorExposureTags() []string {
	vocabulary := goExtractorVocabularyPattern.FindStringSubmatch(goExtractorSource)
	if vocabulary == nil {
		return nil
	}

	values := make(map[string]string)
	for _, declaration := range goExtractorTagValuePattern.FindAllStringSubmatch(goExtractorSource, -1) {
		values[declaration[1]] = declaration[2]
	}

	names := make([]string, 0, 4)

	for _, member := range strings.Split(vocabulary[1], ",") {
		member = strings.TrimSpace(member)
		if member == "" {
			continue
		}

		if value, ok := values[member]; ok {
			names = append(names, value)
		} else {
			names = append(names, member)
		}
	}

	return names
}

// EVERY HALF OF THE EMBEDDED GENERATOR IS HELD TO THE NAMES IT CLAIMS.
//
// The generator is three files — a Node script, the Go extractor it builds for a
// Go action, and the Rust crate it builds for a Rust one — and they are carried
// into this binary by hand from another repository, which neither repository can
// see. That has already produced a fix applied to one of them and to nobody,
// twice. Half a sync is the dangerous shape: it does not fail to build, it makes
// the language an action is written in decide what its author may write.
//
// So each half is pinned to what it claims, rather than the three being compared
// to each other. WHICH HALF DECIDES IS DIFFERENT PER LANGUAGE, and that is the
// thing to know before reading the pins below. For TypeScript and for Rust, the
// script claims the names, validates them and refuses what it will not admit —
// the Rust crate hands it every comment verbatim and states no opinion. For Go,
// the extractor below builds the whole `ai` object itself and the script writes
// its answer through untouched, so the names a Go action may write are the ones
// declared in that file and nowhere else.
//
// Pinning each to its own list is what makes a sync visible: a file arriving
// with a vocabulary this package was not told about fails here, naming the file,
// instead of changing what an author may write in silence.
var goActionTags = []string{"tool", "effects", "retry", "discloses"}

func TestTheGoExtractorClaimsTheVocabularyItOwns(t *testing.T) {
	claimed := goExtractorExposureTags()

	if len(claimed) == 0 {
		t.Fatal("no tag declarations were found in the embedded Go extractor, so this check proves nothing")
	}

	if strings.Join(claimed, ",") != strings.Join(goActionTags, ",") {
		t.Fatalf("the Go extractor claims %v and this package declares %v", claimed, goActionTags)
	}
}

// THE ONE NAME THE RUST COMPANION AND THE SCRIPT BOTH HAVE TO KNOW.
//
// `@Payload` is the only annotation the two of them share, and they use it for
// different halves of one job: the script CLAIMS the name, so a line writing it
// is lifted out of the description instead of reaching a model as a sentence
// about what the action does; the companion RESOLVES it, because the answer is a
// type in the file and only something holding the parsed file can find it.
//
// Spelled differently, neither half fails. The companion reads a payload
// annotation nobody wrote and describes the struct it defaults to, while the
// script leaves the author's line in the prose — a schema for the wrong type and
// a description carrying a directive, from a run that exited zero.
func TestTheRustCompanionAndTheScriptSpellPayloadTheSameWay(t *testing.T) {
	source, err := rustCompanionCrate.ReadFile(rustCompanionEmbedDir + "/src/tags.rs")
	if err != nil {
		t.Fatalf("the embedded Rust companion carries no tags.rs: %v", err)
	}

	resolved := regexp.MustCompile(`PAYLOAD_ANNOTATION: &str = "(\w+)"`).FindStringSubmatch(string(source))
	if resolved == nil {
		t.Fatal("the Rust companion names no payload annotation, so this check proves nothing")
	}

	claimed := regexp.MustCompile(`(?m)^const PAYLOAD_TAG = '(\w+)'$`).FindStringSubmatch(generatorScript)
	if claimed == nil {
		t.Fatal("the script claims no payload annotation, so this check proves nothing")
	}

	if resolved[1] != claimed[1] {
		t.Fatalf("the Rust companion resolves @%s and the script claims @%s", resolved[1], claimed[1])
	}
}

// AND THE SAME STATUS FOR A REFUSAL.
//
// A refusal reaches this process as an exit status and nothing else, and the
// status is what decides whether the action.json a refused source has made
// untrue is discarded or left to be read as current. Four files declare that
// number — this package, the script, and the two extractors it launches — and if
// any of them drifts, a refused source keeps shipping its earlier description
// while its author is told the build failed.
//
// It is the one number every half must agree on whatever else they claim: the
// vocabularies are per language, and this is not. A Rust action refused for
// writing a payload annotation that names nothing has to discard its stale
// description exactly as a Go action refused for a mistyped tag does.
func TestTheRefusalStatusIsOneNumberEverywhere(t *testing.T) {
	rustCompanionSource, err := rustCompanionCrate.ReadFile(rustCompanionEmbedDir + "/src/main.rs")
	if err != nil {
		t.Fatalf("the embedded Rust companion carries no main.rs: %v", err)
	}

	declarations := map[string]*regexp.Regexp{
		"the script":         regexp.MustCompile(`ANNOTATION_REFUSAL_EXIT_CODE = (\d+)`),
		"the Go extractor":   regexp.MustCompile(`annotationRefusalExitCode = (\d+)`),
		"the Rust extractor": regexp.MustCompile(`REFUSAL_EXIT_CODE: u8 = (\d+)`),
	}

	sources := map[string]string{
		"the script":         generatorScript,
		"the Go extractor":   goExtractorSource,
		"the Rust extractor": string(rustCompanionSource),
	}

	for name, pattern := range declarations {
		match := pattern.FindStringSubmatch(sources[name])
		if match == nil {
			t.Fatalf("%s declares no refusal status, so this check proves nothing", name)
		}

		status, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("%s declares a refusal status that is not a number: %v", name, err)
		}

		if status != AnnotationRefusalExitCode {
			t.Fatalf("%s refuses with %d and this package reads %d", name, status, AnnotationRefusalExitCode)
		}
	}
}
