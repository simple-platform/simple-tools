package build

import (
	"errors"
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
// @shortdesc Writes things by name.
// @usewhen A caller names one thing and wants it changed.
// @usewhen A caller corrects a thing it read earlier.
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

	if shortDesc := metadata.value("ai", "shortdesc"); shortDesc != "Writes things by name." {
		t.Fatalf("expected the declared listing line, got %#v", shortDesc)
	}

	// `@usewhen` is the one repeatable name, and every line written reaches the
	// listing in the order it was written.
	if useWhen := metadata.value("ai", "usewhen"); !equalStrings(useWhen,
		"A caller names one thing and wants it changed.",
		"A caller corrects a thing it read earlier.") {
		t.Fatalf("expected the declared triggers in order, got %#v", useWhen)
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

// THE LISTING TAGS WITHOUT `@tool` ARE A STATEMENT ABOUT NOTHING, NOT AN ERROR.
//
// `@shortdesc` and `@usewhen` describe a tool to a model choosing between tools,
// and an action that is not a tool never enters that listing. The platform's
// generator drops them there rather than refusing, and this build runs that
// generator, so it answers the same way: no exposure block, and a description
// the dropped lines have not leaked into.
func TestGoActionDropsListingTagsWrittenWithoutTool(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "query-things", `package main

// Reads things.
//
// @shortdesc Reads things by name.
// @usewhen A caller names one thing and wants the row behind it.
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	metadata := generatedActionMetadata(t, actionDir)

	if _, exposed := metadata["ai"]; exposed {
		t.Fatalf("an action that is not a tool carried an exposure block: %#v", metadata.object("ai"))
	}

	if metadata.description() != "Reads things." {
		t.Fatalf("the dropped listing tags leaked into the description, got %q", metadata.description())
	}
}

// A RETIRED TAG IS PROSE, AND IS LEFT WHERE ITS AUTHOR PUT IT.
//
// `@effects`, `@retry` and `@discloses` were deleted from the vocabulary rather
// than moved anywhere, so nothing claims them: a line writing one is read like
// any other `@name` the build does not own. This copy of the extractor used to
// REQUIRE two of them, which refused every Go tool written in today's
// vocabulary; the pin here is that they neither refuse nor reach the artifact.
func TestGoActionReadsARetiredTagAsProse(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "query-things", `package main

// Reads things.
//
// @tool
// @shortdesc Reads things by name.
// @effects read
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected a retired tag not to stop the build, got %v", err)
	}

	metadata := generatedActionMetadata(t, actionDir)

	if _, carried := metadata.object("ai")["effects"]; carried {
		t.Fatalf("a retired tag reached the exposure block: %#v", metadata.object("ai"))
	}

	if !strings.Contains(metadata.description(), "@effects read") {
		t.Fatalf("a line nothing claims was taken out of the author's prose, got %q", metadata.description())
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
// @usewhen A caller names one thing and wants the row behind it.
// @shortdesc Reads things by name.
// @tool
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

	// Written in the opposite order in the source, so the order below is the
	// file format's and not an echo of the author's.
	want := "\"ai\": {\n    \"tool\": true,\n    \"shortdesc\": \"Reads things by name.\",\n    \"usewhen\": [\n      \"A caller names one thing and wants the row behind it.\"\n    ]\n  }"
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
// @shortdesc Reads things by name.
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
// @shortdesc Reads things by name.
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

	for _, testCase := range malformedStatementCases("// ") {
		t.Run(testCase.name, func(t *testing.T) {
			actionDir := writeGoAction(t, "mutate-things", `package main

// Writes things.
//
`+testCase.statement+`
//
// @Payload Input
func handler() {}
`+payloadStructSource)

			assertRefused(t, ExtractMetadata(fsx.OSFileSystem{}, actionDir), append([]string{"mutate-things"}, testCase.want...))
		})
	}
}

// malformedStatement is one exposure statement the vocabulary refuses, with the
// words its refusal has to carry.
type malformedStatement struct {
	name      string
	statement string
	want      []string
}

// malformedStatementCases is every refusal the vocabulary makes, written with
// the comment prefix of the language under test.
//
// One list for every language, because there is one vocabulary: the same
// statement refused in one language and shipped in another is the drift these
// copies of the generator exist to prevent.
func malformedStatementCases(prefix string) []malformedStatement {
	lines := func(tags ...string) string {
		return prefix + strings.Join(tags, "\n"+prefix)
	}

	return []malformedStatement{
		{
			name:      "a value written after the modifier tag",
			statement: lines("@tool true", "@shortdesc Reads things by name."),
			want:      []string{"modifier tag and takes no value", `"true"`},
		},
		{
			name:      "a tool with no line for the listing to carry",
			statement: lines("@tool"),
			want:      []string{"must declare @shortdesc"},
		},
		{
			name:      "a listing line with nothing after it",
			statement: lines("@tool", "@shortdesc"),
			want:      []string{"@shortdesc is written with nothing after it"},
		},
		{
			name:      "a listing line written twice",
			statement: lines("@tool", "@shortdesc Reads things.", "@shortdesc Reads things by name."),
			want:      []string{"@shortdesc is declared more than once"},
		},
		{
			name:      "a name one edit from a claimed one",
			statement: lines("@tool", "@shortdes Reads things by name."),
			want:      []string{"@shortdes", "one edit from @shortdesc"},
		},
		{
			name:      "a trigger longer than a listing carries",
			statement: lines("@tool", "@shortdesc Reads things by name.", "@usewhen "+strings.Repeat("x", 101)),
			want:      []string{"101 characters", "at most 100"},
		},
		{
			// Refused rather than dropped, unlike the listing tags: an author who
			// wrote both and lost `@tool` has an action that quietly stopped
			// being callable, and this is the one line left that says so.
			name:      "a dispatch claim on an action that is not a tool",
			statement: lines("@parallelsafe", "@shortdesc Reads things by name."),
			want:      []string{"@parallelsafe says how a tool may be dispatched", "Write @tool"},
		},
		{
			name:      "a value written after the dispatch claim",
			statement: lines("@tool", "@shortdesc Reads things by name.", "@parallelsafe reads only"),
			want:      []string{"@parallelsafe is a modifier tag and takes no value", `"reads only"`},
		},
		{
			// Quoted the same way in every language: between plain quotes, with
			// nothing inside escaped. The Go program used to escape it.
			name:      "a quoted value written after the dispatch claim",
			statement: lines("@tool", "@shortdesc Reads things by name.", `@parallelsafe "yes"`),
			want:      []string{"@parallelsafe is a modifier tag and takes no value", `carries ""yes""`},
		},
		{
			name:      "a dispatch claim written twice",
			statement: lines("@tool", "@shortdesc Reads things by name.", "@parallelsafe", "@parallelsafe"),
			want:      []string{"@parallelsafe is declared more than once"},
		},
		{
			name:      "the dispatch claim with an underscore",
			statement: lines("@tool", "@shortdesc Reads things by name.", "@parallel_safe"),
			want:      []string{"@parallel_safe", "one edit from @parallelsafe"},
		},
		{
			name:      "the dispatch claim with a hyphen",
			statement: lines("@tool", "@shortdesc Reads things by name.", "@parallel-safe"),
			want:      []string{"@parallel-safe", "one edit from @parallelsafe"},
		},
		{
			name:      "the dispatch claim in camel case",
			statement: lines("@tool", "@shortdesc Reads things by name.", "@parallelSafe"),
			want:      []string{"@parallelSafe", "one edit from @parallelsafe"},
		},
		{
			name:      "the dispatch claim missing a letter",
			statement: lines("@tool", "@shortdesc Reads things by name.", "@parallelsaf"),
			want:      []string{"@parallelsaf", "one edit from @parallelsafe"},
		},
	}
}

// parallelSafeStatement is a complete statement carrying the dispatch claim,
// written FIRST so the position the claim lands in is the file format's and
// not an echo of the source.
func parallelSafeStatement(prefix string) string {
	return prefix + strings.Join([]string{
		"@parallelsafe",
		"@usewhen A caller names one thing and wants the row behind it.",
		"@shortdesc Reads things by name.",
		"@tool",
	}, "\n"+prefix)
}

// wantParallelSafeBlock is the `ai` block every language writes for
// parallelSafeStatement, byte for byte: `parallelsafe` is the last member, and
// it is `true` because it is present at all.
const wantParallelSafeBlock = "\"ai\": {\n    \"tool\": true,\n    \"shortdesc\": \"Reads things by name.\",\n    \"usewhen\": [\n      \"A caller names one thing and wants the row behind it.\"\n    ],\n    \"parallelsafe\": true\n  }"

// assertParallelSafeClaimCarried holds an action.json to the dispatch claim its
// source made.
func assertParallelSafeClaimCarried(t *testing.T, actionDir string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(actionDir, "action.json"))
	if err != nil {
		t.Fatalf("failed to read action.json: %v", err)
	}

	if !strings.Contains(string(data), wantParallelSafeBlock) {
		t.Fatalf("expected the exposure statement written as\n%s\ngot\n%s", wantParallelSafeBlock, data)
	}

	if description := generatedActionMetadata(t, actionDir).description(); strings.Contains(description, "@") {
		t.Fatalf("expected the claim to be lifted out of the description, got %q", description)
	}
}

// THE DISPATCH CLAIM REACHES THE ARTIFACT AS THE HOST READS IT.
//
// `@parallelsafe` is the one tag written for the host rather than the model:
// it says the tool only reads and may run beside the other parallel-safe calls
// of one batch. The host honours it only when the member is exactly `true`, so
// the shape is the contract — last in the block, present only when written.
func TestGoActionCarriesTheParallelSafeClaimLast(t *testing.T) {
	requireGenerator(t)

	actionDir := writeGoAction(t, "query-things", `package main

// Reads things.
//
`+parallelSafeStatement("// ")+`
//
// @Payload Input
func handler() {}
`+payloadStructSource)

	if err := ExtractMetadata(fsx.OSFileSystem{}, actionDir); err != nil {
		t.Fatalf("expected the action to be described, got %v", err)
	}

	assertParallelSafeClaimCarried(t, actionDir)
}

// assertRefused holds a refusal to every word an author needs from it.
func assertRefused(t *testing.T, err error, want []string) {
	t.Helper()

	if err == nil {
		t.Fatal("expected a refusal")
	}

	var refusal *AnnotationRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("expected an annotation refusal, got %T: %v", err, err)
	}

	for _, word := range want {
		if !strings.Contains(err.Error(), word) {
			t.Fatalf("expected the refusal to mention %q, got %q", word, err.Error())
		}
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
// @tool sometimes
// @shortdesc Writes things by name.
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
// @shortdesc Writes things by name.
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
