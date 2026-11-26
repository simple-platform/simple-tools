package structure

import (
	"strings"
	"testing"
)

const testPersona = "# Support Agent\n\n## Role\n\nCoordinates authorized service work.\n\n## Communication Style\n\nUses clear and direct language.\n"

const testPlaybook = "## Collaboration\n\nAsks a focused question when a request is unclear.\n\n## Domain Boundaries\n\nKeeps decisions within the assigned responsibilities.\n\n## Completion Voice\n\nReports results and the next useful step.\n"

func TestCheckInstructionsRuleCases(t *testing.T) {
	tests := []struct {
		name       string
		persona    string
		playbook   string
		wantText   int
		wantLine   int
		wantRule   string
		wantNoRule bool
	}{
		{name: "invalid encoding", persona: "first line\n\xff\n", playbook: testPlaybook, wantText: 0, wantLine: 2, wantRule: "valid UTF-8"},
		{name: "carriage return", persona: strings.ReplaceAll(testPersona, "\n", "\r\n"), playbook: testPlaybook, wantText: 0, wantLine: 1, wantRule: "carriage return"},
		{name: "missing final newline", persona: strings.TrimSuffix(testPersona, "\n"), playbook: testPlaybook, wantText: 0, wantLine: 9, wantRule: "exactly one line feed"},
		{name: "trailing blank line", persona: testPersona + "\n", playbook: testPlaybook, wantText: 0, wantLine: 10, wantRule: "blank line at the end"},
		{name: "empty stored body", persona: "", playbook: testPlaybook, wantText: 0, wantLine: 0, wantRule: "non-empty guidance body"},
		{name: "assembly over byte cap", persona: testPersona, playbook: overByteLimitPlaybook(), wantText: 1, wantLine: 0, wantRule: "16384-byte limit"},
		{name: "missing name heading", persona: strings.Replace(testPersona, "# Support Agent\n", "", 1), playbook: testPlaybook, wantText: 0, wantLine: 1, wantRule: "display name"},
		{name: "name mismatch", persona: strings.Replace(testPersona, "# Support Agent", "# Support Team", 1), playbook: testPlaybook, wantText: 0, wantLine: 1, wantRule: "match the agent's display name"},
		{name: "multiple name headings", persona: strings.Replace(testPersona, "## Role", "# Another Heading\n\n## Role", 1), playbook: testPlaybook, wantText: 0, wantLine: 3, wantRule: "exactly one first-level heading"},
		{name: "unknown section", persona: testPersona, playbook: testPlaybook + "\n## Notes\n\nExtra context.\n", wantText: 1, wantLine: 13, wantRule: "required section headings"},
		{name: "missing persona section", persona: strings.Replace(testPersona, "## Role\n\nCoordinates authorized service work.\n\n", "", 1), playbook: testPlaybook, wantText: 0, wantLine: 3, wantRule: "missing heading \"## Role\""},
		{name: "duplicate across bodies", persona: testPersona, playbook: "## Role\n\nAnother role description.\n\n" + testPlaybook, wantText: 1, wantLine: 1, wantRule: "only once"},
		{name: "wrong section order", persona: testPersona, playbook: "## Domain Boundaries\n\nKeeps decisions within the assigned responsibilities.\n\n## Collaboration\n\nAsks a focused question when a request is unclear.\n\n## Completion Voice\n\nReports results and the next useful step.\n", wantText: 1, wantLine: 1, wantRule: "place the required headings in this order"},
		{name: "fenced remote import", persona: strings.Replace(testPersona, "Coordinates authorized service work.", "```\n@import 'remote'\n```", 1), playbook: testPlaybook, wantText: 0, wantLine: 6, wantRule: "remote import directives"},
		{name: "fenced headings are ignored", persona: testPersona + "~~~text\n# Hidden\n## Unlisted\n~~~\n", playbook: testPlaybook, wantNoRule: true},
		{name: "heading whitespace is collapsed", persona: strings.Replace(testPersona, "# Support Agent", "# Support   Agent", 1), playbook: testPlaybook, wantNoRule: true},
		{name: "old smaller size bound is not applied", persona: testPersona, playbook: strings.TrimSuffix(testPlaybook, "\n") + strings.Repeat("x", 4_100) + "\n", wantNoRule: true},
	}
	for _, marker := range instructionMarkerCases() {
		tests = append(tests, struct {
			name       string
			persona    string
			playbook   string
			wantText   int
			wantLine   int
			wantRule   string
			wantNoRule bool
		}{
			name:     marker.name,
			persona:  strings.Replace(testPersona, "Coordinates authorized service work.", "Coordinates service work. "+marker.text, 1),
			playbook: testPlaybook,
			wantText: 0,
			wantLine: 5,
			wantRule: marker.rule,
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			personaFindings, playbookFindings := CheckInstructions("Support Agent", test.persona, test.playbook)
			both := append(append([]Finding{}, personaFindings...), playbookFindings...)
			if test.wantNoRule {
				if len(both) != 0 {
					t.Fatalf("expected no findings, got %#v", both)
				}
				return
			}
			found := findRule(personaFindings, playbookFindings, test.wantRule)
			if found == nil {
				t.Fatalf("missing finding containing %q: %#v", test.wantRule, both)
			}
			if found.Line != test.wantLine {
				t.Errorf("finding line = %d, want %d", found.Line, test.wantLine)
			}
			if test.wantText == 0 && !containsFinding(personaFindings, test.wantRule) {
				t.Errorf("finding belongs to playbook, want persona: %#v", both)
			}
			if test.wantText == 1 && !containsFinding(playbookFindings, test.wantRule) {
				t.Errorf("finding belongs to persona, want playbook: %#v", both)
			}
			if !strings.HasSuffix(found.Rule, ".") {
				t.Errorf("finding is not a sentence: %q", found.Rule)
			}
			cleanPersona, cleanPlaybook := CheckInstructions("Support Agent", testPersona, testPlaybook)
			if len(cleanPersona)+len(cleanPlaybook) != 0 {
				t.Errorf("clean counterpart produced findings: %#v %#v", cleanPersona, cleanPlaybook)
			}
		})
	}
}

func TestCheckInstructionsRequiredCleanPairs(t *testing.T) {
	tests := []struct {
		name     string
		persona  string
		playbook string
	}{
		{name: "canonical persona and playbook", persona: testPersona, playbook: testPlaybook},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			personFindings, playFindings := CheckInstructions("Support Agent", test.persona, test.playbook)
			if test.name == "canonical persona and playbook" && len(personFindings)+len(playFindings) != 0 {
				t.Fatalf("canonical pair produced findings: %#v %#v", personFindings, playFindings)
			}
		})
	}

	// A size at the cap passes; bytes above it fail even when both stored bodies
	// are individually below the limit.
	playbook := strings.TrimSuffix(testPlaybook, "\n")
	baseSize := len(assembleInstructions(testPersona, playbook+"\n"))
	playbook = playbook + strings.Repeat("x", instructionMaxBytes-baseSize) + "\n"
	personaFindings, playbookFindings := CheckInstructions("Support Agent", testPersona, playbook)
	if len(personaFindings)+len(playbookFindings) != 0 {
		t.Fatalf("instruction body exactly at the cap was refused: %#v %#v", personaFindings, playbookFindings)
	}
}

func TestCheckInstructionsInvalidBodiesStopAllChecks(t *testing.T) {
	persona := "before\n\xff"
	playbook := "\xfe\n"
	personaFindings, playbookFindings := CheckInstructions("Support Agent", persona, playbook)
	if len(personaFindings) != 1 || personaFindings[0].Line != 2 {
		t.Fatalf("persona encoding finding = %#v", personaFindings)
	}
	if len(playbookFindings) != 1 || playbookFindings[0].Line != 1 {
		t.Fatalf("playbook encoding finding = %#v", playbookFindings)
	}
}

func TestCheckInstructionsFenceCanSpanBodies(t *testing.T) {
	persona := testPersona + "```\n"
	playbook := "## Collaboration\n\nSkipped while the fence is open.\n```\n" + strings.TrimPrefix(testPlaybook, "## Collaboration\n\nAsks a focused question when a request is unclear.\n\n")
	personaFindings, playbookFindings := CheckInstructions("Support Agent", persona, playbook)
	if !containsFinding(playbookFindings, "missing heading \"## Collaboration\"") {
		t.Fatalf("expected headings before fence close to be skipped: %#v %#v", personaFindings, playbookFindings)
	}
}

func TestCheckInstructionsNameWhitespaceAndLocations(t *testing.T) {
	persona := strings.Replace(testPersona, "# Support Agent", "# Support\tAgent", 1)
	personaFindings, playbookFindings := CheckInstructions("Support Agent", persona, testPlaybook)
	if len(personaFindings)+len(playbookFindings) != 0 {
		t.Fatalf("equivalent heading whitespace was refused: %#v %#v", personaFindings, playbookFindings)
	}

	persona = strings.Replace(testPersona, "# Support Agent", "# PRIVATE_HEADING_VALUE", 1)
	persona = strings.Replace(persona, "## Communication Style", "## PRIVATE_SECTION_VALUE", 1)
	personaFindings, playbookFindings = CheckInstructions("Support Agent", persona, testPlaybook)
	for _, finding := range append(personaFindings, playbookFindings...) {
		if strings.Contains(finding.Rule, "PRIVATE_") {
			t.Fatalf("finding echoed body text: %#v", finding)
		}
	}
}

func TestCheckSkillRuleCases(t *testing.T) {
	good := skillBody("skill-refund-policy")
	tests := []struct {
		name     string
		body     string
		wantLine int
		wantRule string
		clean    bool
	}{
		{name: "missing title", body: strings.Replace(good, "# Refund Policy\n", "", 1), wantLine: 1, wantRule: "exactly one title heading"},
		{name: "duplicate title", body: strings.Replace(good, "# Refund Policy\n", "# Refund Policy\n# Other Title\n", 1), wantLine: 2, wantRule: "keep exactly one title heading"},
		{name: "unknown section", body: good + "\n## Extra Notes\n\nAdditional material.\n", wantLine: 35, wantRule: "this section heading is not allowed"},
		{name: "duplicate section", body: strings.Replace(good, "## Good Examples\n", "## Procedure\n\nA second procedure.\n\n## Good Examples\n", 1), wantLine: 20, wantRule: "each section heading only once"},
		{name: "missing required section", body: strings.Replace(good, "## Required Context\n\nNeeded context is listed here.\n\n", "", 1), wantLine: 12, wantRule: "add section \"Required Context\""},
		{name: "required sections out of order", body: swapHeadingNames(good, "When To Use", "Required Context"), wantLine: 9, wantRule: "put the required skill sections in this order"},
		{name: "optional section outside allowed span", body: strings.Replace(good, "## Purpose", "## Domain Terms\n\nDefines domain language.\n\n## Purpose", 1), wantLine: 3, wantRule: "place optional sections after Required Context"},
		{name: "invalid slug", body: strings.Replace(good, "skill-refund-policy", "skill_Refund_policy", 1), wantLine: 7, wantRule: "write a slug line"},
		{name: "empty purpose section", body: "# Refund Policy\n\nSlug: `skill-refund-policy`\n\n## Purpose\n\n## When To Use\n- When a return needs review.\n\n" + strings.Join(skillTail(), "\n"), wantLine: 6, wantRule: "write a paragraph directly under Purpose"},
		{name: "no when to use bullet", body: strings.Replace(good, "- When a return needs review.", "Use this when a return needs review.", 1), wantLine: 10, wantRule: "add at least one dash bullet under When To Use"},
		{name: "empty related tools", body: optionalRelatedTools(good, "A useful tool is described here."), wantLine: 33, wantRule: "add at least one tool identifier"},
		{name: "double space after heading marker", body: strings.Replace(good, "## Purpose", "##  Purpose", 1), wantLine: 9, wantRule: "add section \"Purpose\""},
		{name: "trailing whitespace in heading", body: strings.Replace(good, "## Purpose\n", "## Purpose \n", 1), wantLine: 3, wantRule: "this section heading is not allowed"},
		{name: "tilde fence is not a skill fence", body: strings.Replace(good, "## Procedure\n", "## Procedure\n~~~\n## Hidden Section\n~~~\n", 1), wantLine: 18, wantRule: "this section heading is not allowed"},
		{name: "optional section in allowed position", body: withDomainTerms(good), clean: true},
		{name: "fenced heading text is ignored", body: strings.Replace(good, "## Procedure\n", "## Procedure\n```text\n## Hidden Section\n# Also Hidden\n```\n", 1), clean: true},
		{name: "tool slug is valid", body: skillBody("tool-find-order"), clean: true},
		{name: "no final newline is accepted", body: strings.TrimSuffix(good, "\n"), clean: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			findings := CheckSkill(test.body)
			if test.clean {
				if len(findings) != 0 {
					t.Fatalf("valid skill produced findings: %#v", findings)
				}
				return
			}
			finding := findSkillRule(findings, test.wantRule)
			if finding == nil {
				t.Fatalf("missing finding containing %q: %#v", test.wantRule, findings)
			}
			if finding.Line != test.wantLine {
				t.Errorf("finding line = %d, want %d", finding.Line, test.wantLine)
			}
			if !strings.HasSuffix(finding.Rule, ".") {
				t.Errorf("finding is not a sentence: %q", finding.Rule)
			}
			if len(CheckSkill(good)) != 0 {
				t.Errorf("clean counterpart produced findings: %#v", CheckSkill(good))
			}
		})
	}
}

func TestCheckSkillPurposeUsesFirstParagraph(t *testing.T) {
	body := strings.Replace(skillBody("skill-refund-policy"), "## Purpose\n\nExplains the review process.\n\n", "## Purpose\n\nFirst paragraph.\n\nA later paragraph.\n\n", 1)
	if findings := CheckSkill(body); len(findings) != 0 {
		t.Fatalf("purpose with a later paragraph was refused: %#v", findings)
	}
}

func TestCheckSkillSlugMustBeOutsideFence(t *testing.T) {
	body := strings.Replace(skillBody("skill-refund-policy"), "Slug: `skill-refund-policy`", "```\nSlug: `skill-refund-policy`\n```", 1)
	if finding := findSkillRule(CheckSkill(body), "write a slug line"); finding == nil {
		t.Fatal("slug inside a fenced block was accepted")
	}
}

func overByteLimitPlaybook() string {
	base := strings.TrimSuffix(testPlaybook, "\n")
	baseSize := len(assembleInstructions(testPersona, base+"\n"))
	return base + strings.Repeat("x", instructionMaxBytes+1-baseSize) + "\n"
}

func instructionMarkerCases() []struct{ name, text, rule string } {
	return []struct{ name, text, rule string }{
		{name: "remote style import", text: "@import 'asset'", rule: "remote import directives"},
		{name: "server include", text: "<!--#include file='asset'", rule: "server-side include directives"},
		{name: "template include", text: "{{ include asset }}", rule: "template include directives"},
		{name: "script markup", text: "<script type='text/plain'>", rule: "executable script markup"},
		{name: "script link", text: "javascript:run()", rule: "script-based links"},
		{name: "template block", text: "<% run %>", rule: "template code"},
		{name: "interpolation", text: "${value}", rule: "interpolated template expressions"},
		{name: "event handler", text: "onclick =run()", rule: "inline event handlers"},
		{name: "data uri", text: "data:text/plain;base64,QQ==", rule: "embedded base64 data"},
		{name: "bare base64 marker", text: ";base64,QQ==", rule: "base64-encoded content"},
	}
}

func skillBody(slug string) string {
	return "# Refund Policy\n\n## Purpose\n\nExplains the review process.\n\nSlug: `" + slug + "`\n\n" + strings.Join(skillTail(), "\n") + "\n"
}

func skillTail() []string {
	return []string{
		"## When To Use",
		"- When a return needs review.",
		"",
		"## Required Context",
		"",
		"Needed context is listed here.",
		"",
		"## Procedure",
		"",
		"Review the request and record the decision.",
		"",
		"## Good Examples",
		"",
		"A complete request includes its order details.",
		"",
		"## Bad Examples And Why",
		"",
		"A missing order reference blocks the review.",
		"",
		"## Error Recovery",
		"",
		"Ask for the missing information and try again.",
		"",
		"## Completion Checklist",
		"- Confirm the decision and required next step.",
	}
}

func withDomainTerms(body string) string {
	return strings.Replace(body, "## Procedure", "## Domain Terms\n\nA return is a request to send an item back.\n\n## Procedure", 1)
}

func optionalRelatedTools(body, content string) string {
	return strings.Replace(body, "## Completion Checklist", "## Related Tools\n\n- "+content+"\n\n## Completion Checklist", 1)
}

func swapHeadingNames(body, left, right string) string {
	temporary := "## TEMPORARY HEADING"
	body = strings.Replace(body, "## "+left, temporary, 1)
	body = strings.Replace(body, "## "+right, "## "+left, 1)
	return strings.Replace(body, temporary, "## "+right, 1)
}

func findRule(persona, playbook []Finding, text string) *Finding {
	for _, findings := range [][]Finding{persona, playbook} {
		for i := range findings {
			if strings.Contains(findings[i].Rule, text) {
				return &findings[i]
			}
		}
	}
	return nil
}

func containsFinding(findings []Finding, text string) bool {
	return findSkillRule(findings, text) != nil
}

func findSkillRule(findings []Finding, text string) *Finding {
	for i := range findings {
		if strings.Contains(findings[i].Rule, text) {
			return &findings[i]
		}
	}
	return nil
}
