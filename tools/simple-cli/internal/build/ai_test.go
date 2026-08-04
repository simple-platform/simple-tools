package build

import (
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
