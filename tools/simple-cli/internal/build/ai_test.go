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
	declared := generatorExposureTags()

	if len(declared) == 0 {
		t.Fatal("no tag declarations were found in the embedded generator, so this check proves nothing")
	}

	if strings.Join(declared, ",") != strings.Join(ExposureTagNames(), ",") {
		t.Fatalf("the generator enforces %v and this package declares %v", declared, ExposureTagNames())
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
	vocabulary := goExtractorVocabularyPattern.FindStringSubmatch(extractGoDocContent)
	if vocabulary == nil {
		return nil
	}

	values := make(map[string]string)
	for _, declaration := range goExtractorTagValuePattern.FindAllStringSubmatch(extractGoDocContent, -1) {
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

// BOTH HALVES OF THE EMBEDDED GENERATOR CLAIM THE SAME VOCABULARY.
//
// The generator is two files — a Node script and the Go extractor it builds for
// a Go action — and they are carried into this binary by hand from another
// repository, which neither repository can see. That has already produced a fix
// applied to one of them and to nobody, twice. Half a sync is the dangerous
// shape: it does not fail to build, it makes the language an action is written
// in decide which tags exist.
func TestBothHalvesOfTheGeneratorClaimTheSameVocabulary(t *testing.T) {
	claimed := goExtractorExposureTags()

	if len(claimed) == 0 {
		t.Fatal("no tag declarations were found in the embedded Go extractor, so this check proves nothing")
	}

	if strings.Join(claimed, ",") != strings.Join(generatorExposureTags(), ",") {
		t.Fatalf("the Go extractor claims %v and the script claims %v", claimed, generatorExposureTags())
	}
}

// AND THE SAME STATUS FOR A REFUSAL.
//
// A refusal reaches this process as an exit status and nothing else, and the
// status is what decides whether the action.json a refused source has made
// untrue is discarded or left to be read as current. Three files declare that
// number — this package, the script, and the Go extractor it launches — and if
// any of them drifts, a refused source keeps shipping its earlier description
// while its author is told the build failed.
func TestTheRefusalStatusIsOneNumberEverywhere(t *testing.T) {
	declarations := map[string]*regexp.Regexp{
		"the script":       regexp.MustCompile(`ANNOTATION_REFUSAL_EXIT_CODE = (\d+)`),
		"the Go extractor": regexp.MustCompile(`annotationRefusalExitCode = (\d+)`),
	}

	sources := map[string]string{
		"the script":       extractScriptContent,
		"the Go extractor": extractGoDocContent,
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
