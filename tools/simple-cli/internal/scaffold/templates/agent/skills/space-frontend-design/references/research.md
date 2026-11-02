# Frontend design sources and Simple decisions

Refreshed October 27, 2026 after the generated Spaces failed visual review.
The earlier review covered vendor catalogs and community skills but missed the
Codex core prompt's direct frontend guidance. This refresh reads the actual
published design instructions, distinguishes skills from prompts, and corrects
that omission. Sources below were fetched from GitHub and read; none was
installed or copied wholesale.

## Primary source code

Links use the revisions inspected so later upstream changes do not alter this
record. Claude's two published skill files matched at these revisions.

### Claude

[Anthropic frontend-design skill](https://github.com/anthropics/skills/blob/dbd4588f9e1033efb41dad4bef2f7947c8993d44/skills/frontend-design/SKILL.md); [Claude Code plugin skill](https://github.com/anthropics/claude-code/blob/08c6a4f5121ae140d3c1b5c96e3e5ae53587b54b/plugins/frontend-design/skills/frontend-design/SKILL.md)

Ground design in the subject; compare compositions before building; make typography and structure intentional; concentrate emphasis; critique screenshots; edit interface copy.

### Codex

[Core prompt, Frontend tasks](https://github.com/openai/codex/blob/2cab11ca40aad193382348a7bda46da2fc47596a/codex-rs/core/gpt-5.2-codex_prompt.md#frontend-tasks)

Avoid interchangeable layouts; choose a clear direction; work on desktop and mobile. Its existing-design-system exception explicitly preserves established patterns and visual language. This is a prompt section, not a standalone skill.

### Codex skills

[Figma generate-design](https://github.com/openai/skills/blob/49f948faa9258a0c61caceaf225e179651397431/skills/.curated/figma-generate-design/SKILL.md); [Figma design-system rules](https://github.com/openai/skills/blob/49f948faa9258a0c61caceaf225e179651397431/skills/.curated/figma-create-design-system-rules/SKILL.md); [Figma implement-design](https://github.com/openai/skills/blob/49f948faa9258a0c61caceaf225e179651397431/skills/.curated/figma-implement-design/SKILL.md)

Inspect components and tokens; reuse the project's visual language; validate the rendered result. Figma is appropriate when requested or supplied, not required for an ordinary Space.

### Google portable design skills

[Stitch taste-design](https://github.com/google-labs-code/stitch-skills/blob/0337446dadde6f8c94210444e2aa9d546126480f/plugins/stitch-utilities/skills/taste-design/SKILL.md); [generate-design](https://github.com/google-labs-code/stitch-skills/blob/0337446dadde6f8c94210444e2aa9d546126480f/plugins/stitch-design/skills/generate-design/SKILL.md); [design-md](https://github.com/google-labs-code/stitch-skills/blob/0337446dadde6f8c94210444e2aa9d546126480f/plugins/stitch-utilities/skills/design-md/SKILL.md); [shadcn-ui](https://github.com/google-labs-code/stitch-skills/blob/0337446dadde6f8c94210444e2aa9d546126480f/plugins/stitch-build/skills/shadcn-ui/SKILL.md)

Separate established design tokens from layout/content instructions; retain design context across screens; discover component variants and states; create deliberate hierarchy and proportion. These skills support multiple harnesses, including Gemini CLI.

### Gemini CLI

[Prompt snippets](https://github.com/google-gemini/gemini-cli/blob/363a13119a57354f469732577518b9d6b319d1d6/packages/core/src/prompts/snippets.ts)

Functional implementation, consistent spacing, feedback and build validation. Generic app/prototype instructions are not a specialized enterprise frontend skill.

Catalogs inspected: `anthropics/skills`, `anthropics/claude-code`, `openai/codex`,
`openai/skills`, `google-labs-code/stitch-skills`, `google-gemini/gemini-cli`, and
`google-gemini/gemini-skills`. The inspected OpenAI skills catalog has no generic
`frontend-design/SKILL.md`. The Gemini skills catalog at
`ee9da52195752ca83c2fca701d96bcb5a9c08d2d` supplies API skills, not a web design
recipe. Gemini CLI's [frontend](https://github.com/google-gemini/gemini-cli/blob/363a13119a57354f469732577518b9d6b319d1d6/.gemini/commands/frontend.toml)
and [review-frontend](https://github.com/google-gemini/gemini-cli/blob/363a13119a57354f469732577518b9d6b319d1d6/.gemini/commands/review-frontend.toml)
commands concern that repository's terminal UI; they are not web frontend skills.
These are conclusions about the inspected catalogs, not every custom skill.

## What changed in Simple's skill

The old skill was stronger on platform correctness than visual composition. The
Sales dashboard exposed the gap: lengthy filter chrome, equally emphasized
summary cards, duplicate stage bar/donut views, and records buried below the
first viewport. Correct tokens and components did not make that composition good.

| Design decision            | Concrete instruction                                                                                                                                                                        |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Start with the work        | Specify the user's decision, real content, dominant region, first viewport and interaction before coding. Compare plausible compositions for substantial new work.                          |
| Give content visual weight | Use deliberate proportions and type roles; quiet supporting context; compact filters; inspect combined spacing. A queue/detail workspace and a dependency view need different compositions. |
| Make richness useful       | Use real relationships, business details and connected screens. Choose complementary analytical questions; do not fill chart slots with the same breakdown.                                 |
| Keep identity stable       | Reuse tenant theme/fonts and organization components while varying composition. Record the app's chosen visual contract in its existing KB.                                                 |
| Reuse component states     | Inspect installed and shared component variants before building local controls; reuse a fit and add only missing behavior.                                                                   |
| Evaluate the actual result | Critique rendered hierarchy, composition, craft, domain specificity and continuity. Functional success alone does not finish a design. Check mobile states and interactions too.            |

The existing platform constraints remain: managed forms and collections, host
ownership, authorized queries, checked persistence and draft preservation.
Exact APIs remain in public docs and the installed SDK, rather than being
reproduced as a second manual in the entrypoint.

## Guidance deliberately excluded

- Claude's new palette/typeface selection and Codex's default theme variation
  yield to existing tenant branding, consistent with Codex's explicit exception.
- Stitch taste-design's font bans, required new palette, perpetual micro-motion,
  and mandatory staggered lists are not enterprise Space requirements. Nor is
  forced asymmetry or zero-radius styling. Useful hierarchy can come from several
  compositions within the existing tokens.
- Marketing heroes, decorative atmosphere, blanket animation and arbitrary
  component/style installation do not improve an operational workflow by default.
- No new Figma/Stitch dependency or generic app scaffold is introduced.

## Earlier supporting research

The October 26 review also read [Vercel Web Design Guidelines](https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/skills/web-design-guidelines/SKILL.md),
[Vercel React Best Practices](https://github.com/vercel-labs/agent-skills/blob/063bee94c3f4df8453406c830b0a7df0f2860278/skills/react-best-practices/SKILL.md),
[Copilot anti-ui-slop](https://github.com/github/awesome-copilot/blob/920e3b927a553642834af13097f53f6f716947db/skills/anti-ui-slop/SKILL.md),
[Copilot UX/UI agent](https://github.com/github/awesome-copilot/blob/920e3b927a553642834af13097f53f6f716947db/agents/se-ux-ui-designer.agent.md),
[UI UX Pro Max](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill/blob/master/.claude/skills/ui-ux-pro-max/SKILL.md),
and [shadcn theming](https://ui.shadcn.com/docs/theming). Those supplied useful
accessibility, state, overflow and component-reuse checks. They were not reread
as part of this refresh. Cursor/OpenCode skill documentation explained harness
discovery; it was not evidence of a vendor frontend aesthetic skill.

Independent CRM and incident-workspace composition trials
exercise the revised skill; their observations and integration limits belong in
the fixture app KB. Two trials cannot establish reliable quality across all apps.
