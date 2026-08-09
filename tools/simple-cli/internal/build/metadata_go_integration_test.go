package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"simple-cli/internal/fsx"
)

// A GO ACTION IS DESCRIBED BY THE SAME GENERATOR AS EVERY OTHER ACTION, AND
// THESE RUN IT.
//
// These properties used to be held against seven hundred lines of Go in this
// package that read the same doc comments a second time. Nothing could reach
// that code — the build refuses an action without `src/index.ts` before
// extraction runs — so what it proved was that a second, unreachable reading of
// the contract agreed with its own tests.
//
// Running the real generator proves the thing that matters instead: that this
// tool and the platform's build produce the same file from the same source. It
// also exercises what a unit test could not — the generator finding the Go
// extractor beside itself, which this tool used to write to a different
// directory entirely.
func writeGoAction(t *testing.T, name, source string) string {
	t.Helper()

	actionDir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(actionDir, 0755); err != nil {
		t.Fatalf("failed to create the action directory: %v", err)
	}

	if err := os.WriteFile(filepath.Join(actionDir, "main.go"), []byte(source), 0644); err != nil {
		t.Fatalf("failed to write the action source: %v", err)
	}

	return actionDir
}

func requireGenerator(t *testing.T) {
	t.Helper()

	if err := checkNodeJS(); err != nil {
		t.Skip("Node.js not available, skipping integration test")
	}
}

const payloadStructSource = "\ntype Input struct {\n\tName string `json:\"name\"`\n}\n"

// Whether an agent may call an action is stated in the action's own doc comment
// and carried into action.json by the build.
//
// The alternative was a hand-added key in a generated file: the build rewrites
// that file wholesale, so the statement survived only until the next author
// touched the action, and the only thing keeping an action from becoming a tool
// was its absence from a list kept somewhere else.
func TestGoActionCarriesTheExposureStatement(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "mutate-things", `package main

// Writes things.
//
// @tool
// @effects write, destructive
// @retry never
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	metadata := generatedActionMetadata(t, actionDir)

	if metadata.value("ai", "tool") != true {
		t.Fatalf("expected a tool, got %#v", metadata.object("ai"))
	}

	if effects := metadata.value("ai", "effects"); !equalStrings(effects, "write", "destructive") {
		t.Fatalf("expected the declared effects in order, got %#v", effects)
	}

	if retry := metadata.value("ai", "retry"); retry != "never" {
		t.Fatalf("expected the declared retry, got %v", retry)
	}

	// The class an action discloses defaults to the widest one, so an author who
	// says nothing is not silently credited with saying the narrowest.
	if discloses := metadata.value("ai", "discloses"); discloses != "tenant_record" {
		t.Fatalf("expected the default disclosure, got %v", discloses)
	}

	// The description is what the model reads as the tool's own statement about
	// itself. An annotation left in it ships as part of that statement.
	if strings.Contains(metadata.description(), "@") {
		t.Fatalf("expected every annotation to be lifted out of the description, got %q", metadata.description())
	}

	if metadata.description() != "Writes things." {
		t.Fatalf("expected the description to survive intact, got %q", metadata.description())
	}
}

func TestGoActionThatDeclaresNothingCarriesNoStatement(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "send-things", `package main

// Sends things.
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	if _, exposed := generatedActionMetadata(t, actionDir)["ai"]; exposed {
		t.Fatal("expected an action that declares nothing to carry no ai key")
	}
}

// THE ARTIFACT SPELLS THE VOCABULARY THE WAY THE SOURCE DOES.
//
// action.json is read by hosts that never see the doc comment it came from, so a
// member named one thing in the source and another in the file gives those two
// readers different words for one fact. The names and their order are the file
// format, held against the bytes on disk rather than against a struct tag.
func TestExposureStatementIsWrittenWithTheVocabularysOwnNames(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "query-things", `package main

// Reads things.
//
// @tool
// @effects read
// @retry verify-first
// @discloses settings_field
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	data, err := os.ReadFile(filepath.Join(actionDir, "action.json"))
	if err != nil {
		t.Fatalf("failed to read action.json: %v", err)
	}

	want := "\"ai\": {\n    \"tool\": true,\n    \"effects\": [\n      \"read\"\n    ],\n    \"retry\": \"verify-first\",\n    \"discloses\": \"settings_field\"\n  }"
	if !strings.Contains(string(data), want) {
		t.Fatalf("expected the exposure statement written as\n%s\ngot\n%s", want, data)
	}
}

// WHERE AN AUTHOR WRITES THE STATEMENT MUST NOT DECIDE WHETHER IT IS HEARD.
//
// The statement is written here in the position that was heard by nothing: a
// comment above the package clause. Which block supplies the DESCRIPTION is
// settled by rules about where a payload is declared, and letting those rules
// also decide where an exposure statement counts drops a tag written anywhere
// else in silence — a dropped `@tool` is an action that quietly stops being
// callable.
func TestGoActionHearsAStatementWrittenAboveThePackageClause(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "query-things", `// The things module.
//
// @tool
// @effects read
// @retry safe
package main

// Reads things.
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	metadata := generatedActionMetadata(t, actionDir)

	if metadata.value("ai", "tool") != true {
		t.Fatalf("a statement written above the package clause was dropped: %#v", metadata.object("ai"))
	}

	if metadata.description() != "Reads things." {
		t.Fatalf("expected the describing block to still supply the description, got %q", metadata.description())
	}
}

// AN ANNOTATION BLOCK DOES NOT HAVE TO BE THE LAST THING IN A DOC COMMENT.
//
// An author may state the tags and keep writing, and what follows is part of
// what the tool says it does. Read as the text BEFORE the first tag, that
// trailing paragraph is not merely misplaced, it is deleted — and the sentence
// most likely to be written there is the one that says what the action refuses,
// which is exactly the rule a planner needs before it acts on an empty answer.
func TestGoActionKeepsTheDescriptionWrittenAfterTheAnnotations(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "query-things", `package main

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
`+payloadStructSource)

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	metadata := generatedActionMetadata(t, actionDir)

	if !strings.Contains(metadata.description(), "REFUSED rather than answered") {
		t.Fatalf("the description written after the annotations was dropped, got %q", metadata.description())
	}

	if strings.Contains(metadata.description(), "@") {
		t.Fatalf("expected every annotation to be lifted out of the description, got %q", metadata.description())
	}
}

// A MALFORMED STATEMENT REFUSES, AND THE REFUSAL REACHES THE AUTHOR WHOLE.
//
// The generator runs in a child process, so its sentence has a boundary to
// survive. What an author gets told is the only thing that makes the failure
// actionable: the action, the tag, and what would have been accepted instead.
func TestGoActionRefusesAMalformedExposureStatement(t *testing.T) {
	requireGenerator(t)

	cases := []struct {
		name      string
		statement string
		want      []string
	}{
		{
			name:      "a qualifier without the modifier it qualifies",
			statement: "// @effects read\n// @retry safe",
			want:      []string{"mutate-things", "@tool", "says nothing"},
		},
		{
			name:      "an effect outside the vocabulary",
			statement: "// @tool\n// @effects read, sideways\n// @retry safe",
			want:      []string{"mutate-things", `unknown effect "sideways"`, "read, orchestration, write, destructive, external, credential"},
		},
		{
			name:      "a value written after the modifier tag",
			statement: "// @tool true\n// @effects read\n// @retry safe",
			want:      []string{"mutate-things", "modifier tag and takes no value", `"true"`},
		},
		{
			name:      "a name one edit from a claimed one",
			statement: "// @tool\n// @effects read\n// @retry safe\n// @dicloses secret_field",
			want:      []string{"mutate-things", "@dicloses", "one edit from @discloses"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			actionDir := writeGoAction(t, "mutate-things", `package main

// Writes things.
//
`+testCase.statement+`
//
// @Payload Input
func handler() {}
`+payloadStructSource)

			err := ExtractMetadata(fsx.OSFileSystem{}, actionDir)
			if err == nil {
				t.Fatal("expected a refusal")
			}

			for _, want := range testCase.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("expected the refusal to mention %q, got %q", want, err.Error())
				}
			}
		})
	}
}

// A REFUSED SOURCE TAKES ITS STALE OUTPUT WITH IT.
//
// action.json is generated wholesale from the source beside it, so the copy left
// behind by a refusal was generated from an EARLIER source: it describes an
// action that no longer exists and carries the exposure statement its author has
// since tried to change. Nothing downstream can tell — a well-formed file reads
// as current — so a rejected edit ships as though it had been accepted.
func TestARefusedGoSourceDiscardsTheActionJSONItHasMadeUntrue(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "mutate-things", `package main

// Writes things.
//
// @tool
// @effects write, sideways
// @retry never
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	stalePath := filepath.Join(actionDir, "action.json")
	if err := os.WriteFile(stalePath, []byte(`{"description":"Writes things.","ai":{"tool":true}}`), 0644); err != nil {
		t.Fatalf("failed to write the stale metadata: %v", err)
	}

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err == nil {
		t.Fatal("expected a refusal")
	}

	if _, err := os.Stat(stalePath); err == nil {
		t.Fatal("the refused source left its earlier description shipping")
	}
}

// A GENERATOR THAT COULD NOT RUN IS NOT A REFUSAL, and the difference is what
// the file on disk is worth. An absent toolchain says nothing about whether the
// action.json beside the source is true; deleting every action's metadata
// because `go` is missing turns one environment problem into a working tree
// nobody can build from. The build fails either way, so nothing ships unverified
// on the strength of a file that was left alone.
func TestAGeneratorThatCouldNotRunLeavesTheActionJSONAlone(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "mutate-things", `package main

// Writes things.
//
// @tool
// @effects write
// @retry never
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	described, err := os.ReadFile(filepath.Join(actionDir, "action.json"))
	if err != nil {
		t.Fatalf("failed to read action.json: %v", err)
	}

	t.Setenv("PATH", brokenGoToolchain(t)+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err == nil {
		t.Fatal("expected the extraction to fail")
	}

	kept, err := os.ReadFile(filepath.Join(actionDir, "action.json"))
	if err != nil {
		t.Fatalf("a generator that could not run took the action's description with it: %v", err)
	}

	if string(kept) != string(described) {
		t.Fatalf("a generator that could not run rewrote the action's description:\n%s\n---\n%s", kept, described)
	}
}

// brokenGoToolchain is a directory holding a `go` that fails for a reason that is
// not a refusal.
func brokenGoToolchain(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	shim := filepath.Join(dir, "go")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\necho 'go: cannot find the toolchain' >&2\nexit 1\n"), 0755); err != nil {
		t.Fatalf("failed to write the shim: %v", err)
	}

	return dir
}

// equalStrings is whether a decoded JSON array holds exactly these strings, in
// this order.
func equalStrings(decoded any, want ...string) bool {
	values, isArray := decoded.([]any)
	if !isArray || len(values) != len(want) {
		return false
	}

	for index, value := range values {
		if value != want[index] {
			return false
		}
	}

	return true
}
