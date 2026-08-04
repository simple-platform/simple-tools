package build

import (
	"encoding/json"
	"strings"
	"testing"
)

// Whether an agent may call an action is stated in the action's own doc comment
// and carried into action.json by the build.
//
// The alternative was a hand-added key in a generated file: the build rewrites
// that file wholesale, so the statement survived only until the next author
// touched the action, and the only thing keeping an action from becoming a tool
// was its absence from a list kept somewhere else.
func TestExtractGoMetadataCarriesTheExposureStatement(t *testing.T) {
	fs := &MockFileSystem{files: map[string]string{
		"/actions/mutate-things/main.go": `package main

// Writes things.
//
// @tool
// @effects write, destructive
// @retry never
//
// @Payload Input
func handler() {}

type Input struct {
	Name string ` + "`json:\"name\" jsonschema:\"required\"`" + `
}
`,
	}}

	metadata, err := extractGoMetadata(fs, "/actions/mutate-things")
	if err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	if metadata.AI == nil {
		t.Fatal("expected an exposure statement")
	}

	if !metadata.AI.Tool {
		t.Fatalf("expected a tool, got %#v", metadata.AI)
	}

	if strings.Join(metadata.AI.Effects, ",") != "write,destructive" {
		t.Fatalf("expected the declared effects in order, got %#v", metadata.AI.Effects)
	}

	if metadata.AI.Retry != "never" {
		t.Fatalf("expected the declared retry, got %q", metadata.AI.Retry)
	}

	if metadata.AI.Discloses != defaultDiscloses {
		t.Fatalf("expected the default disclosure, got %q", metadata.AI.Discloses)
	}

	// The description is what the model reads as the tool's own statement about
	// itself. An annotation left in it ships as part of that statement.
	if strings.Contains(metadata.Description, "@") {
		t.Fatalf("expected every annotation to be lifted out of the description, got %q", metadata.Description)
	}

	if metadata.Description != "Writes things." {
		t.Fatalf("expected the description to survive intact, got %q", metadata.Description)
	}
}

func TestExtractGoMetadataOmitsTheStatementForAnUnannotatedAction(t *testing.T) {
	fs := &MockFileSystem{files: map[string]string{
		"/actions/send-things/main.go": `package main

// Sends things.
//
// @Payload Input
func handler() {}

type Input struct {
	Name string ` + "`json:\"name\"`" + `
}
`,
	}}

	metadata, err := extractGoMetadata(fs, "/actions/send-things")
	if err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	if metadata.AI != nil {
		t.Fatalf("expected no exposure statement, got %#v", metadata.AI)
	}

	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("failed to marshal metadata: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("failed to decode metadata: %v", err)
	}

	if _, exposed := decoded["ai"]; exposed {
		t.Fatalf("expected an action that declares nothing to carry no ai key, got %#v", decoded["ai"])
	}
}

// THE ARTIFACT SPELLS THE VOCABULARY THE WAY THE SOURCE DOES.
//
// action.json is read by hosts that never see the doc comment it came from, so
// a member named one thing in the source and another in the file gives those two
// readers different words for one fact. The names and their order are the file
// format, held here rather than left to whichever struct tag was edited last.
func TestExposureStatementIsWrittenWithTheVocabularysOwnNames(t *testing.T) {
	encoded, err := json.Marshal(&AIMetadata{
		Tool:      true,
		Effects:   []string{"read"},
		Retry:     "verify-first",
		Discloses: "settings_field",
	})
	if err != nil {
		t.Fatalf("failed to marshal the exposure statement: %v", err)
	}

	want := `{"tool":true,"effects":["read"],"retry":"verify-first","discloses":"settings_field"}`
	if string(encoded) != want {
		t.Fatalf("expected %s, got %s", want, encoded)
	}
}

func TestExtractGoMetadataRefusesAMalformedExposureStatement(t *testing.T) {
	fs := &MockFileSystem{files: map[string]string{
		"/actions/mutate-things/main.go": `package main

// Writes things.
//
// @tool
//
// @Payload Input
func handler() {}

type Input struct {
	Name string ` + "`json:\"name\"`" + `
}
`,
	}}

	metadata, err := extractGoMetadata(fs, "/actions/mutate-things")
	if err == nil {
		t.Fatalf("expected a refusal, got %#v", metadata)
	}

	for _, want := range []string{"mutate-things", "@effects", "read, orchestration, write, destructive, external, credential"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected the refusal to mention %q, got %q", want, err.Error())
		}
	}
}

// A REFUSED SOURCE TAKES ITS STALE OUTPUT WITH IT.
//
// action.json is generated wholesale from the source beside it, so the copy
// left behind by a refusal was generated from an EARLIER source: it describes an
// action that no longer exists and carries the exposure statement its author has
// since tried to change. Nothing downstream can tell — a well-formed file reads
// as current — so a rejected edit ships as though it had been accepted.
func TestExtractMetadataDiscardsTheActionJSONARefusedSourceHasMadeUntrue(t *testing.T) {
	fs := &MockFileSystem{files: map[string]string{
		"/actions/mutate-things/main.go": `package main

// Writes things.
//
// @tool
// @effects write, sideways
// @retry never
//
// @Payload Input
func handler() {}

type Input struct {
	Name string ` + "`json:\"name\"`" + `
}
`,
		"/actions/mutate-things/action.json": `{"description":"Writes things.","ai":{"tool":true,"effects":["write"]}}`,
	}}

	if err := ExtractMetadata(fs, "/actions/mutate-things"); err == nil {
		t.Fatal("expected a refusal")
	}

	if stale, kept := fs.files["/actions/mutate-things/action.json"]; kept {
		t.Fatalf("the refused source left its earlier description shipping: %s", stale)
	}
}

// A GENERATOR THAT COULD NOT RUN IS NOT A REFUSAL, and the difference is what
// the file on disk is worth. An absent toolchain says nothing about whether the
// action.json beside the source is true; deleting every action's metadata
// because `node` is missing turns one environment problem into a working tree
// nobody can build from. The build fails either way, so nothing ships unverified
// on the strength of a file that was left alone.
func TestExtractMetadataKeepsTheActionJSONWhenTheGeneratorMerelyFailed(t *testing.T) {
	fs := &MockFileSystem{files: map[string]string{
		"/actions/mutate-things/main.go":     "package main\n\nfunc handler() {}\n",
		"/actions/mutate-things/action.json": `{"description":"Writes things."}`,
	}}

	if err := ExtractMetadata(fs, "/actions/mutate-things"); err == nil {
		t.Fatal("expected the extraction to fail")
	}

	if _, kept := fs.files["/actions/mutate-things/action.json"]; !kept {
		t.Fatal("a generator that could not run took the action's description with it")
	}
}

func TestBuildAIMetadataRefusesMalformedAnnotations(t *testing.T) {
	cases := []struct {
		name string
		tags []exposureTag
		want string
	}{
		{
			name: "an effect outside the vocabulary",
			tags: []exposureTag{
				{name: "tool"},
				{name: "effects", value: "read, sideways"},
				{name: "retry", value: "safe"},
			},
			want: `unknown effect "sideways"`,
		},
		{
			name: "the same effect twice",
			tags: []exposureTag{
				{name: "tool"},
				{name: "effects", value: "read, read"},
				{name: "retry", value: "safe"},
			},
			want: `names "read" twice`,
		},
		{
			name: "two retries",
			tags: []exposureTag{
				{name: "tool"},
				{name: "effects", value: "read"},
				{name: "retry", value: "safe"},
				{name: "retry", value: "never"},
			},
			want: "declared more than once",
		},
		{
			name: "a tool that states no retry",
			tags: []exposureTag{
				{name: "tool"},
				{name: "effects", value: "read"},
			},
			want: "@retry",
		},
		// The value this tag used to take. It is refused rather than translated,
		// because a vocabulary that quietly accepts the old spelling is one nobody
		// finishes migrating.
		{
			name: "a retry outside the vocabulary",
			tags: []exposureTag{
				{name: "tool"},
				{name: "effects", value: "read"},
				{name: "retry", value: "never_automatic"},
			},
			want: `@retry takes "never_automatic". Accepted: safe, keyed, verify-first, never`,
		},
		{
			name: "a disclosure outside the vocabulary",
			tags: []exposureTag{
				{name: "tool"},
				{name: "effects", value: "read"},
				{name: "retry", value: "safe"},
				{name: "discloses", value: "audit_log"},
			},
			want: `@discloses takes "audit_log"`,
		},
		{
			name: "qualifications without the exposure marker",
			tags: []exposureTag{
				{name: "effects", value: "read"},
				{name: "retry", value: "safe"},
			},
			want: "declares @effects, @retry without @tool",
		},
		// The boolean this tag used to take. Read as `true` it would migrate
		// itself, and `@tool false` would then read as an exposed action.
		{
			name: "a modifier tag carrying the value it used to take",
			tags: []exposureTag{
				{name: "tool", value: "true"},
				{name: "effects", value: "read"},
				{name: "retry", value: "safe"},
			},
			want: `@tool is a modifier tag and takes no value, and this one carries "true"`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ai, err := buildAIMetadata("some-action", testCase.tags)

			if err == nil {
				t.Fatalf("expected a refusal, got %#v", ai)
			}

			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected the refusal to mention %q, got %q", testCase.want, err.Error())
			}

			if !strings.Contains(err.Error(), "some-action") {
				t.Fatalf("expected the refusal to name the action, got %q", err.Error())
			}
		})
	}
}

// ONLY THE FOUR NAMES ARE CLAIMED; EVERY OTHER TAG IS THE AUTHOR'S.
//
// This vocabulary shares a doc comment with `@Payload` here and with `@param`,
// `@remarks` and the rest of TSDoc on the other generator. One that lifted every
// `@` line out of the description to be sure of catching its own would delete an
// author's prose to protect itself — and the deleted line is the one the model
// most needed, because a tag is where a rule tends to be written down.
func TestExposureTagsAreClaimedByNameAndNothingElseIs(t *testing.T) {
	for _, line := range []string{"@tool", "@effects read", "@retry safe", "@discloses secret_field"} {
		if _, claimed := exposureTagFromDocLine(line); !claimed {
			t.Fatalf("expected %q to be read as an exposure annotation", line)
		}
	}

	for _, line := range []string{"@param name the thing", "@remarks read this", "@toolbox", "@maxLength 40", "Reads things."} {
		if tag, claimed := exposureTagFromDocLine(line); claimed {
			t.Fatalf("expected %q to be left to the description, got %#v", line, tag)
		}
	}
}

// AN ANNOTATION BLOCK DOES NOT HAVE TO BE THE LAST THING IN A DOC COMMENT.
//
// An author may state the tags and keep writing, and what follows is part of
// what the tool says it does. Read as the text BEFORE the first tag, that
// trailing paragraph is not merely misplaced, it is deleted — and the sentence
// most likely to be written there is the one that says what the action refuses,
// which is exactly the rule a planner needs before it acts on an empty answer.
func TestExtractGoMetadataKeepsTheDescriptionWrittenAfterTheAnnotations(t *testing.T) {
	fs := &MockFileSystem{files: map[string]string{
		"/actions/query-things/main.go": `package main

// Reads things.
//
// @tool
// @effects read
// @retry safe
//
// A name matching no row is REFUSED rather than answered with an empty
// result, so an empty answer is never false good news.
//
// @Payload Input
func handler() {}

type Input struct {
	Name string ` + "`json:\"name\"`" + `
}
`,
	}}

	metadata, err := extractGoMetadata(fs, "/actions/query-things")
	if err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	if !strings.Contains(metadata.Description, "REFUSED rather than answered") {
		t.Fatalf("the description written after the annotations was dropped, got %q", metadata.Description)
	}

	if strings.Contains(metadata.Description, "@") {
		t.Fatalf("expected every annotation to be lifted out of the description, got %q", metadata.Description)
	}
}

// WHERE AN AUTHOR WRITES THE STATEMENT MUST NOT DECIDE WHETHER IT IS HEARD.
//
// Which block supplies the DESCRIPTION is settled by rules about where a
// payload is declared. Letting those rules also decide where an exposure
// statement counts drops a tag written anywhere else in silence, and a dropped
// `@tool` is an action that quietly stops being callable.
func TestExtractGoMetadataHearsAnAnnotationWrittenOutsideTheDescribingBlock(t *testing.T) {
	fs := &MockFileSystem{files: map[string]string{
		"/actions/query-things/main.go": `package main

// Reads things.
//
// @Payload Input
func handler() {}

// The shape the host resolves.
//
// @tool
// @effects read
// @retry safe
type Resolved struct{}

type Input struct {
	Name string ` + "`json:\"name\"`" + `
}
`,
	}}

	metadata, err := extractGoMetadata(fs, "/actions/query-things")
	if err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	if metadata.AI == nil || !metadata.AI.Tool {
		t.Fatalf("an exposure statement written outside the describing block was dropped: %#v", metadata.AI)
	}

	if metadata.Description != "Reads things." {
		t.Fatalf("expected the describing block to still supply the description, got %q", metadata.Description)
	}
}
