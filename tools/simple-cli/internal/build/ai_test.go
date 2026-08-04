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
// @ai_tool true
// @ai_effects write, destructive
// @ai_retry_safety never_automatic
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

	if metadata.AI.RetrySafety != "never_automatic" {
		t.Fatalf("expected the declared retry safety, got %q", metadata.AI.RetrySafety)
	}

	if metadata.AI.DisclosureOrigin != aiDefaultDisclosureOrigin {
		t.Fatalf("expected the default disclosure origin, got %q", metadata.AI.DisclosureOrigin)
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

func TestExtractGoMetadataRefusesAMalformedExposureStatement(t *testing.T) {
	fs := &MockFileSystem{files: map[string]string{
		"/actions/mutate-things/main.go": `package main

// Writes things.
//
// @ai_tool true
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

	for _, want := range []string{"mutate-things", "@ai_effects", "read, orchestration, write, destructive, external, credential"} {
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
// @ai_tool true
// @ai_effects write, sideways
// @ai_retry_safety never_automatic
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
		tags []aiTag
		want string
	}{
		{
			name: "an effect outside the vocabulary",
			tags: []aiTag{
				{name: "ai_tool", value: "true"},
				{name: "ai_effects", value: "read, sideways"},
				{name: "ai_retry_safety", value: "safe"},
			},
			want: `unknown effect "sideways"`,
		},
		{
			name: "the same effect twice",
			tags: []aiTag{
				{name: "ai_tool", value: "true"},
				{name: "ai_effects", value: "read, read"},
				{name: "ai_retry_safety", value: "safe"},
			},
			want: `names "read" twice`,
		},
		{
			name: "two retry safeties",
			tags: []aiTag{
				{name: "ai_tool", value: "true"},
				{name: "ai_effects", value: "read"},
				{name: "ai_retry_safety", value: "safe"},
				{name: "ai_retry_safety", value: "never_automatic"},
			},
			want: "declared more than once",
		},
		{
			name: "a tool that states no retry safety",
			tags: []aiTag{
				{name: "ai_tool", value: "true"},
				{name: "ai_effects", value: "read"},
			},
			want: "@ai_retry_safety",
		},
		{
			name: "a disclosure origin outside the vocabulary",
			tags: []aiTag{
				{name: "ai_tool", value: "true"},
				{name: "ai_effects", value: "read"},
				{name: "ai_retry_safety", value: "safe"},
				{name: "ai_disclosure_origin", value: "audit_log"},
			},
			want: `@ai_disclosure_origin takes "audit_log"`,
		},
		{
			name: "a misspelled annotation",
			tags: []aiTag{
				{name: "ai_tool", value: "true"},
				{name: "ai_effect", value: "read"},
				{name: "ai_retry_safety", value: "safe"},
			},
			want: "not an exposure annotation",
		},
		{
			name: "qualifications without the exposure marker",
			tags: []aiTag{
				{name: "ai_effects", value: "read"},
				{name: "ai_retry_safety", value: "safe"},
			},
			want: "without @ai_tool",
		},
		{
			name: "an exposure marker that is not a boolean",
			tags: []aiTag{
				{name: "ai_tool", value: "yes"},
				{name: "ai_effects", value: "read"},
				{name: "ai_retry_safety", value: "safe"},
			},
			want: `@ai_tool takes "yes"`,
		},
		{
			name: "qualifications on an action that is not a tool",
			tags: []aiTag{
				{name: "ai_tool", value: "false"},
				{name: "ai_effects", value: "read"},
			},
			want: "@ai_tool is false",
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
// @ai_tool true
// @ai_effects read
// @ai_retry_safety safe
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
// `@ai_tool` is an action that quietly stops being callable.
func TestExtractGoMetadataHearsAnAnnotationWrittenOutsideTheDescribingBlock(t *testing.T) {
	fs := &MockFileSystem{files: map[string]string{
		"/actions/query-things/main.go": `package main

// Reads things.
//
// @Payload Input
func handler() {}

// The shape the host resolves.
//
// @ai_tool true
// @ai_effects read
// @ai_retry_safety safe
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
