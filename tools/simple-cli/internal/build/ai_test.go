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

// goExtractorTagPattern finds the vocabulary as the embedded Go extractor
// declares it.
var goExtractorTagPattern = regexp.MustCompile(`(?m)^\t(\w+)Tag\s+= "([a-z]+)"$`)

// BOTH HALVES OF THE EMBEDDED GENERATOR CLAIM THE SAME VOCABULARY.
//
// The generator is two files — a Node script and the Go extractor it builds for
// a Go action — and they are carried into this binary by hand from another
// repository, which neither repository can see. That has already produced a fix
// applied to one of them and to nobody, twice. Half a sync is the dangerous
// shape: it does not fail to build, it makes the language an action is written
// in decide which tags exist.
func TestBothHalvesOfTheGeneratorClaimTheSameVocabulary(t *testing.T) {
	matches := goExtractorTagPattern.FindAllStringSubmatch(extractGoDocContent, -1)

	claimed := make([]string, 0, len(matches))
	for _, match := range matches {
		claimed = append(claimed, match[2])
	}

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
