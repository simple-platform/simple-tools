// Package structure checks written guidance against the platform's rules.
//
// It works on text alone and leaves file and line ownership to its caller.
package structure

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Finding is one broken rule. Line is 1-based within the text it was found
// in, or 0 when the rule is about the text as a whole.
type Finding struct {
	Line int
	Rule string
}

const instructionMaxBytes = 16_384

var instructionSections = []string{
	"Role",
	"Communication Style",
	"Collaboration",
	"Domain Boundaries",
	"Completion Voice",
}

var instructionHeadingRE = regexp.MustCompile(`^(#+)[ \t]+(\S.*)$`)
var fenceRE = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})[ \\t]*(.*?)[ \\t]*$")

type heading struct {
	level int
	text  string
	line  int
}

type textHeading struct {
	heading
	textIndex int
}

type fence struct {
	char rune
	len  int
}

// CheckInstructions checks a persona and playbook joined in that order.
// Per-text findings use the source text's own line numbers.
func CheckInstructions(displayName, persona, playbook string) (personaFindings, playbookFindings []Finding) {
	bodies := []string{persona, playbook}
	findings := [2][]Finding{}

	// Ill-formed text stops all further checks, as the platform's rules cannot
	// safely interpret headings or assemble it.
	for i, body := range bodies {
		if !utf8.ValidString(body) {
			findings[i] = append(findings[i], Finding{
				Line: lineAtByte(body, firstInvalidUTF8(body)),
				Rule: "write valid UTF-8 text throughout this guidance body",
			})
		}
	}
	if len(findings[0])+len(findings[1]) != 0 {
		return finalize(findings[0]), finalize(findings[1])
	}

	for i, body := range bodies {
		if strings.ContainsRune(body, '\r') {
			findings[i] = append(findings[i], Finding{
				Line: lineAtByte(body, strings.IndexByte(body, '\r')),
				Rule: "remove carriage return characters and keep line feeds as the only line ending",
			})
		}
		if body != "" {
			switch {
			case !strings.HasSuffix(body, "\n"):
				findings[i] = append(findings[i], Finding{
					Line: strings.Count(body, "\n") + 1,
					Rule: "end this guidance body with exactly one line feed",
				})
			case strings.HasSuffix(body, "\n\n"):
				findings[i] = append(findings[i], Finding{
					Line: strings.Count(body, "\n"),
					Rule: "remove the blank line at the end and keep exactly one final line feed",
				})
			}
		}
		if len(body) == 0 {
			findings[i] = append(findings[i], Finding{
				Line: 0,
				Rule: "write a non-empty guidance body",
			})
		}
	}

	assembly := assembleInstructions(persona, playbook)
	if len(assembly) > instructionMaxBytes {
		findings[1] = append(findings[1], Finding{
			Line: 0,
			Rule: "shorten the persona (" + byteCount(len(persona)) + " bytes) and playbook (" + byteCount(len(playbook)) + " bytes); together they use " + byteCount(len(assembly)) + " bytes including the separator, above the 16384-byte limit",
		})
	}

	allHeadings := instructionHeadingsPair(bodies)
	var h1 []textHeading
	var h2 []textHeading
	for _, h := range allHeadings {
		switch h.level {
		case 1:
			h1 = append(h1, h)
		case 2:
			h2 = append(h2, h)
		}
	}

	switch len(h1) {
	case 0:
		findings[0] = append(findings[0], Finding{
			Line: 1,
			Rule: "put the agent's display name, " + quoted(displayName) + ", in the first heading",
		})
	case 1:
		if normalizeWhitespace(h1[0].text) != normalizeWhitespace(displayName) {
			findings[h1[0].textIndex] = append(findings[h1[0].textIndex], Finding{
				Line: h1[0].line,
				Rule: "make the first heading match the agent's display name, " + quoted(displayName) + ", ignoring repeated whitespace only",
			})
		}
	default:
		findings[h1[1].textIndex] = append(findings[h1[1].textIndex], Finding{
			Line: h1[1].line,
			Rule: "keep exactly one first-level heading for the agent's display name",
		})
	}

	for _, h := range h2 {
		if !contains(instructionSections, h.text) {
			findings[h.textIndex] = append(findings[h.textIndex], Finding{
				Line: h.line,
				Rule: "use one of the required section headings: Role, Communication Style, Collaboration, Domain Boundaries or Completion Voice",
			})
		}
	}

	texts := make([]string, 0, len(h2))
	for _, h := range h2 {
		texts = append(texts, h.text)
	}
	for _, required := range instructionSections {
		if !contains(texts, required) {
			owner, line := missingInstructionSection(required, bodies)
			findings[owner] = append(findings[owner], Finding{
				Line: line,
				Rule: "add the missing heading " + quoted("## "+required) + " in the " + instructionOwner(required),
			})
		}
		if count(texts, required) > 1 {
			for n, h := range h2 {
				if h.text == required && countHeadingBefore(h2, n, required) > 0 {
					findings[h.textIndex] = append(findings[h.textIndex], Finding{
						Line: h.line,
						Rule: "keep the required heading " + quoted("## "+required) + " only once",
					})
					break
				}
			}
		}
	}

	knownInOrder := make([]textHeading, 0, len(h2))
	seen := map[string]bool{}
	for _, h := range h2 {
		if contains(instructionSections, h.text) && !seen[h.text] {
			knownInOrder = append(knownInOrder, h)
			seen[h.text] = true
		}
	}
	expected := make([]string, 0, len(knownInOrder))
	for _, required := range instructionSections {
		if seen[required] {
			expected = append(expected, required)
		}
	}
	if !sameHeadings(knownInOrder, expected) {
		bad := firstOrderMismatch(knownInOrder, expected)
		if bad >= 0 {
			h := knownInOrder[bad]
			findings[h.textIndex] = append(findings[h.textIndex], Finding{
				Line: h.line,
				Rule: "place the required headings in this order: Role, Communication Style, Collaboration, Domain Boundaries, then Completion Voice",
			})
		}
	}

	for _, marker := range forbiddenMarkers {
		for i, body := range bodies {
			line := firstMatchingLine(body, marker.pattern)
			if line != 0 {
				findings[i] = append(findings[i], Finding{Line: line, Rule: marker.message})
				break
			}
		}
	}

	return finalize(findings[0]), finalize(findings[1])
}

// CheckSkill checks the canonical structure of a skill or tool lesson.
func CheckSkill(body string) []Finding {
	lines := strings.Split(body, "\n")
	unfenced := skillUnfenced(lines)
	findings := make([]Finding, 0)
	titles := make([]int, 0)
	headingLines := make([]skillHeading, 0)
	for i, line := range unfenced {
		if skillTitleRE.MatchString(line.text) {
			titles = append(titles, i)
		}
		if match := skillHeadingRE.FindStringSubmatch(line.text); match != nil {
			headingLines = append(headingLines, skillHeading{text: match[1], line: line.number})
		}
	}
	if len(titles) == 0 {
		findings = append(findings, Finding{Line: 1, Rule: "add exactly one title heading in the form # Title"})
	} else if len(titles) > 1 {
		findings = append(findings, Finding{Line: unfenced[titles[1]].number, Rule: "keep exactly one title heading in the form # Title"})
	}

	sectionTexts := make([]string, len(headingLines))
	for i, h := range headingLines {
		sectionTexts[i] = h.text
	}
	known := append(append([]string{}, skillRequiredSections...), skillOptionalSections...)
	for _, h := range headingLines {
		if !contains(known, h.text) {
			findings = append(findings, Finding{
				Line: h.line,
				Rule: "this section heading is not allowed; use Purpose, When To Use, Required Context, Procedure, Good Examples, Bad Examples And Why, Error Recovery, Completion Checklist, Domain Terms, Related Tools, Evidence Requirements or Security Notes",
			})
			break
		}
	}
	seen := map[string]bool{}
	for _, h := range headingLines {
		if seen[h.text] {
			findings = append(findings, Finding{Line: h.line, Rule: "keep each section heading only once"})
			break
		}
		seen[h.text] = true
	}

	presentRequired := make([]string, 0, len(skillRequiredSections))
	for _, h := range headingLines {
		if contains(skillRequiredSections, h.text) {
			presentRequired = append(presentRequired, h.text)
		}
	}
	missingRequired := make([]string, 0)
	for _, required := range skillRequiredSections {
		if !contains(presentRequired, required) {
			missingRequired = append(missingRequired, required)
			line := missingSkillSectionLine(required, headingLines, len(lines))
			findings = append(findings, Finding{
				Line: line,
				Rule: "add section " + quoted(required) + "; every skill needs Purpose, When To Use, Required Context, Procedure, Good Examples, Bad Examples And Why, Error Recovery and Completion Checklist in that order",
			})
		}
	}
	if len(missingRequired) == 0 && !sameStrings(presentRequired, skillRequiredSections) {
		line := firstRequiredOrderError(headingLines)
		findings = append(findings, Finding{Line: line, Rule: "put the required skill sections in this order: Purpose, When To Use, Required Context, Procedure, Good Examples, Bad Examples And Why, Error Recovery, then Completion Checklist"})
	}

	for headingPosition, h := range headingLines {
		if contains(skillOptionalSections, h.text) {
			open := headingIndex(sectionTexts, "Required Context")
			close := headingIndex(sectionTexts, "Completion Checklist")
			index := headingPosition
			if open >= 0 && close >= 0 && (index < open || index > close) {
				findings = append(findings, Finding{Line: h.line, Rule: "place optional sections after Required Context and before Completion Checklist"})
				break
			}
		}
	}

	if findCanonicalSlug(unfenced) == "" {
		findings = append(findings, Finding{Line: slugProblemLine(unfenced), Rule: "write a slug line as Slug: `skill-name` or Slug: `tool-name`, using lowercase letters, digits and single hyphens"})
	}

	if contains(sectionTexts, "Purpose") {
		purpose := skillSection(lines, "Purpose")
		start := 0
		for start < len(purpose) && purpose[start].text == "" {
			start++
		}
		if start == len(purpose) {
			findings = append(findings, Finding{Line: sectionLine(headingLines, "Purpose", len(lines)), Rule: "write a paragraph directly under Purpose before the next blank line"})
		}
	}

	if contains(sectionTexts, "When To Use") && len(skillBullets(skillSection(lines, "When To Use"))) == 0 {
		findings = append(findings, Finding{Line: sectionLine(headingLines, "When To Use", len(lines)), Rule: "add at least one dash bullet under When To Use"})
	}
	if contains(sectionTexts, "Related Tools") {
		tools := 0
		relatedTools := skillSection(lines, "Related Tools")
		for _, line := range relatedTools {
			if skillToolRE.MatchString(line.text) {
				tools++
			}
		}
		if len(relatedTools) > 0 && tools == 0 {
			findings = append(findings, Finding{Line: sectionLine(headingLines, "Related Tools", len(lines)), Rule: "add at least one tool identifier as a dash bullet with the identifier in backticks"})
		}
	}
	return finalize(findings)
}

// HasSlugLine reports whether a body contains a Slug line outside a code fence.
func HasSlugLine(body string) bool {
	for _, line := range skillUnfenced(strings.Split(body, "\n")) {
		if strings.HasPrefix(line.text, "Slug:") {
			return true
		}
	}
	return false
}

// SkillFileNameFinding reports when a skill or lesson file does not match its slug.
func SkillFileNameFinding(body, filePath string) *Finding {
	if filePath == "" {
		return nil
	}
	slug := findCanonicalSlug(skillUnfenced(strings.Split(body, "\n")))
	if slug == "" {
		return nil
	}
	want := slug + ".md"
	actual := filePath
	if index := strings.LastIndex(actual, "/"); index >= 0 {
		actual = actual[index+1:]
	}
	if actual == want {
		return nil
	}
	return &Finding{Line: slugLine(body), Rule: "rename " + actual + " to " + want + " so the file name matches its Slug line"}
}

func slugLine(body string) int {
	for _, line := range skillUnfenced(strings.Split(body, "\n")) {
		if strings.HasPrefix(line.text, "Slug:") {
			return line.number
		}
	}
	return 1
}

func finalize(findings []Finding) []Finding {
	for i := range findings {
		findings[i].Rule = strings.TrimSuffix(strings.TrimSpace(findings[i].Rule), ".")
	}
	return findings
}

func assembleInstructions(persona, playbook string) string {
	joined := strings.TrimRightFunc(persona+"\n\n"+playbook, unicode.IsSpace)
	return joined + "\n"
}

func instructionHeadings(body string) []heading {
	lines := strings.Split(body, "\n")
	result := make([]heading, 0)
	var open *fence
	for lineNo, line := range lines {
		if open != nil {
			if closesInstructionFence(line, *open) {
				open = nil
			}
			continue
		}
		if opened := openInstructionFence(line); opened != nil {
			open = opened
			continue
		}
		if match := instructionHeadingRE.FindStringSubmatch(line); match != nil {
			result = append(result, heading{level: len(match[1]), text: strings.TrimSpace(match[2]), line: lineNo + 1})
		}
	}
	return result
}

func instructionHeadingsPair(bodies []string) []textHeading {
	result := make([]textHeading, 0)
	var open *fence
	for textIndex, body := range bodies {
		for lineNo, line := range strings.Split(body, "\n") {
			if open != nil {
				if closesInstructionFence(line, *open) {
					open = nil
				}
				continue
			}
			if opened := openInstructionFence(line); opened != nil {
				open = opened
				continue
			}
			if match := instructionHeadingRE.FindStringSubmatch(line); match != nil {
				result = append(result, textHeading{
					heading:   heading{level: len(match[1]), text: strings.TrimSpace(match[2]), line: lineNo + 1},
					textIndex: textIndex,
				})
			}
		}
	}
	return result
}

func openInstructionFence(line string) *fence {
	match := fenceRE.FindStringSubmatch(line)
	if match == nil {
		return nil
	}
	if match[1][0] == '`' && strings.Contains(match[2], "`") {
		return nil
	}
	return &fence{char: rune(match[1][0]), len: len(match[1])}
}

func closesInstructionFence(line string, opening fence) bool {
	match := fenceRE.FindStringSubmatch(line)
	return match != nil && rune(match[1][0]) == opening.char && len(match[1]) >= opening.len && match[2] == ""
}

func normalizeWhitespace(value string) string { return strings.Join(strings.Fields(value), " ") }

func missingInstructionSection(name string, bodies []string) (int, int) {
	owner := 1
	if name == "Role" || name == "Communication Style" {
		owner = 0
	}
	localOrder := []string{"Collaboration", "Domain Boundaries", "Completion Voice"}
	if owner == 0 {
		localOrder = []string{"Role", "Communication Style"}
	}
	headings := instructionHeadings(bodies[owner])
	position := -1
	for i, expected := range localOrder {
		if expected == name {
			position = i
			break
		}
	}
	for later := position + 1; later < len(localOrder); later++ {
		for _, h := range headings {
			if h.text == localOrder[later] {
				return owner, h.line
			}
		}
	}
	for _, h := range headings {
		if position > 0 && h.text == localOrder[position-1] {
			return owner, h.line + 1
		}
	}
	return owner, len(strings.Split(bodies[owner], "\n"))
}

func instructionOwner(name string) string {
	if name == "Role" || name == "Communication Style" {
		return "persona"
	}
	return "playbook"
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func count(values []string, value string) int {
	result := 0
	for _, candidate := range values {
		if candidate == value {
			result++
		}
	}
	return result
}

func countHeadingBefore(values []textHeading, index int, name string) int {
	result := 0
	for i := 0; i < index; i++ {
		if values[i].text == name {
			result++
		}
	}
	return result
}

func sameHeadings(actual []textHeading, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for i := range actual {
		if actual[i].text != expected[i] {
			return false
		}
	}
	return true
}

func firstOrderMismatch(actual []textHeading, expected []string) int {
	for i := range actual {
		if actual[i].text != expected[i] {
			return i
		}
	}
	return -1
}

func lineAtByte(body string, offset int) int {
	if offset < 0 {
		return 1
	}
	if offset > len(body) {
		offset = len(body)
	}
	return strings.Count(body[:offset], "\n") + 1
}

func firstInvalidUTF8(body string) int {
	for i := 0; i < len(body); {
		_, size := utf8.DecodeRuneInString(body[i:])
		if size == 1 && body[i] >= utf8.RuneSelf {
			return i
		}
		i += size
	}
	return -1
}

func byteCount(size int) string {
	return strconv.Itoa(size)
}

func quoted(value string) string { return "\"" + value + "\"" }

type forbiddenMarker struct {
	pattern *regexp.Regexp
	message string
}

var forbiddenMarkers = []forbiddenMarker{
	{regexp.MustCompile(`(?i)@import`), "remove remote import directives and include the needed content directly"},
	{regexp.MustCompile(`(?i)<!--#include`), "remove server-side include directives and write the needed guidance directly"},
	{regexp.MustCompile(`(?i)\{\{\s*include`), "remove template include directives and write the needed guidance directly"},
	{regexp.MustCompile(`(?i)<script`), "remove executable script markup and describe the guidance in plain text"},
	{regexp.MustCompile(`(?i)javascript:`), "remove script-based links and use plain descriptive text"},
	{regexp.MustCompile(`<%`), "remove template code and write the intended guidance as plain text"},
	{regexp.MustCompile(`\$\{`), "remove interpolated template expressions and write the intended value as plain text"},
	{regexp.MustCompile(`(?i)(?:^|[^a-z])on[a-z]+\s*=`), "remove inline event handlers and describe the behavior in plain text"},
	{regexp.MustCompile(`(?i)data:[^\s"'<>]*;base64,`), "remove embedded base64 data and refer to an approved external asset by name"},
	{regexp.MustCompile(`(?i);base64,`), "remove base64-encoded content and explain the needed information in plain text"},
}

func firstMatchingLine(body string, pattern *regexp.Regexp) int {
	for index, line := range strings.Split(body, "\n") {
		if pattern.MatchString(line) {
			return index + 1
		}
	}
	return 0
}

var skillTitleRE = regexp.MustCompile(`^# (\S.*)$`)
var skillHeadingRE = regexp.MustCompile(`^## (\S.*)$`)
var skillSlugRE = regexp.MustCompile("^Slug: `((?:skill|tool)-[a-z0-9]+(?:-[a-z0-9]+)*)`$")
var skillBulletRE = regexp.MustCompile(`^- (\S.*)$`)
var skillToolRE = regexp.MustCompile("^- `(\\S+)`$")

var skillRequiredSections = []string{
	"Purpose",
	"When To Use",
	"Required Context",
	"Procedure",
	"Good Examples",
	"Bad Examples And Why",
	"Error Recovery",
	"Completion Checklist",
}

var skillOptionalSections = []string{"Domain Terms", "Related Tools", "Evidence Requirements", "Security Notes"}

type skillLine struct {
	text   string
	number int
	fenced bool
}

type skillHeading struct {
	text string
	line int
}

func skillUnfenced(lines []string) []skillLine {
	result := make([]skillLine, 0, len(lines))
	inside := false
	for i, line := range lines {
		isFence := strings.HasPrefix(line, "```")
		if !inside && !isFence {
			result = append(result, skillLine{text: line, number: i + 1})
		}
		if isFence {
			inside = !inside
		}
	}
	return result
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func missingSkillSectionLine(name string, headings []skillHeading, totalLines int) int {
	index := -1
	for i, value := range skillRequiredSections {
		if value == name {
			index = i
			break
		}
	}
	for _, expected := range skillRequiredSections[index+1:] {
		for _, h := range headings {
			if h.text == expected {
				return h.line
			}
		}
	}
	return totalLines + 1
}

func firstRequiredOrderError(headings []skillHeading) int {
	wanted := 0
	for _, h := range headings {
		if !contains(skillRequiredSections, h.text) {
			continue
		}
		if wanted < len(skillRequiredSections) && h.text != skillRequiredSections[wanted] {
			return h.line
		}
		wanted++
	}
	return 0
}

func headingIndex(headings []string, name string) int {
	for i, value := range headings {
		if value == name {
			return i
		}
	}
	return -1
}

func skillSection(lines []string, name string) []skillLine {
	fenced := make([]skillLine, len(lines))
	inside := false
	for i, line := range lines {
		isFence := strings.HasPrefix(line, "```")
		fenced[i] = skillLine{text: line, number: i + 1, fenced: inside || isFence}
		if isFence {
			inside = !inside
		}
	}
	start := -1
	for i, line := range fenced {
		if !line.fenced && line.text == "## "+name {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	end := start
	for end < len(fenced) {
		if !fenced[end].fenced && strings.HasPrefix(fenced[end].text, "## ") {
			break
		}
		end++
	}
	return fenced[start:end]
}

func skillBullets(lines []skillLine) []string {
	result := make([]string, 0)
	for _, line := range lines {
		if match := skillBulletRE.FindStringSubmatch(line.text); match != nil {
			result = append(result, match[1])
			continue
		}
		if len(result) == 0 {
			continue
		}
		trimmed := strings.TrimSpace(line.text)
		if trimmed != "" {
			result[len(result)-1] += " " + trimmed
		}
	}
	return result
}

func findCanonicalSlug(lines []skillLine) string {
	for _, line := range lines {
		if match := skillSlugRE.FindStringSubmatch(line.text); match != nil {
			return match[1]
		}
	}
	return ""
}

func slugProblemLine(lines []skillLine) int {
	for _, line := range lines {
		if strings.HasPrefix(line.text, "Slug:") {
			return line.number
		}
	}
	return 0
}

func sectionLine(headings []skillHeading, name string, totalLines int) int {
	for _, h := range headings {
		if h.text == name {
			return h.line + 1
		}
	}
	return totalLines + 1
}
