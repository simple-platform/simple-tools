# The action exposure vocabulary

An action becomes callable by an agent because **its own source says so**. The
statement is a few doc-comment tags; the build lifts them out of the doc comment
and writes them into `action.json` beside the description and the schema.

Carrying it in the source is what lets regeneration keep it. `action.json` is
rewritten wholesale on every build, so a key added to that file by hand survives
only until the next author touches the action.

`simple build` describes an action with the platform's own generators, carried
into this binary byte for byte (`internal/build/scripts/`). What is written here
is what those generators accept, and an action builds the same way here as it
does on the platform.

## The tags

```ts
/**
 * Reads the risk register for a site.
 *
 * @tool
 * @shortdesc Read the risk register for one site.
 * @usewhen The user asks which risks a site carries.
 * @usewhen A plan needs the open risks before it schedules work.
 */
export interface Payload { site_id: string }
```

| tag             | kind     | required                    | what it carries                                                     |
| --------------- | -------- | --------------------------- | ------------------------------------------------------------------- |
| `@tool`         | modifier | —                           | nothing: the tag carries no value                                   |
| `@shortdesc`    | block    | when `@tool` is present     | the one line a tool listing shows, at most 300 characters           |
| `@usewhen`      | block    | optional, repeatable        | when to reach for the tool, at most 10 lines of 100 characters each |
| `@parallelsafe` | modifier | optional, only with `@tool` | nothing: the tool only reads, so it may overlap parallel-safe calls |
| `@Payload`      | block    | optional                    | the name of the type the input schema is read from                  |

`@shortdesc` and `@usewhen` are written for the **model**: the listing an agent
chooses from carries them on every turn, and the doc comment's own prose arrives
only once the tool is chosen. That is why the widths are limits rather than
advice, and why a statement that does not fit is refused instead of cut short —
a cap nobody was told about is a line the author reads in the source and the
model never sees.

`@Payload` says nothing about exposure. It names the type the schema is read
from, for an author whose input type is not called `Payload`, and it is claimed
so that a directive to the build does not stay in the prose and reach a model as
a sentence about what the action does.

A tool's revision is **not** in this vocabulary. The host pins it, so there is
nothing here for an author to get wrong about it.

### `@parallelsafe`: a claim about concurrency and nothing else

Write `@parallelsafe` when the tool **only reads**: it changes no stored data,
sends nothing outward, and starts no other action that does. It is the one tag
written for the **host** rather than the model — the listing a model chooses
from does not show it — and it lets the host run the call at the same time as
the other parallel-safe calls of the same batch.

```ts
/**
 * Reads the risk register for a site.
 *
 * @tool
 * @shortdesc Read the risk register for one site.
 * @parallelsafe
 */
```

The same line works in every language: ` * @parallelsafe` in a TypeScript JSDoc
block, `// @parallelsafe` in a Go comment, `/// @parallelsafe` in a Rust doc
comment. In Rust it belongs to the action, not to a payload member:
`#[simple(parallelsafe)]` is refused by the SDK macro, as `tool`, `shortdesc`
and `usewhen` are.

**What it controls:** only whether a call may overlap others. Consecutive
parallel-safe calls in a batch form one group that runs together, and their
results come back in the order the model asked for them. Every other call runs
alone, in the place the model put it; reads are never moved ahead of a write.

**What it never controls:** retry and effects. A parallel-safe call is still one
whose effect the host does not know, so what a model is told a failed call did
to stored data is unchanged, and a failed call is never retried because it
carries the tag.

**Nothing checks it, and a wrong claim is the author's.** The build cannot tell
whether a tool only reads, and the platform does not try. A tool that writes
while claiming `@parallelsafe` can overlap another call of its group, so a read
beside it may see the data before or after the write. That is a defect in the
action, not a broken platform guarantee.

It is admitted knowing why the three retired tags below were removed: an author
claim that let a host **repeat** an effect was a hole. `@parallelsafe` is a
claim of the same unverifiable kind, bounded so that hole stays closed — it
governs overlap only.

### What an author no longer writes

`@effects`, `@retry` and `@discloses` were removed from the platform on
2026-08-09. They were not moved anywhere: nothing downstream states them about a
tool, and no action in any language has a way to declare them. The copy of the
generators in this binary still required two of them until it was re-synced, so
every Go tool written in today's vocabulary was refused here and accepted by the
platform's build.

A line writing one of them is now prose, like any other `@name` the build does
not claim: it stays where its author put it and ships as part of the
description. Delete it. `@short_desc` and `@when_use`, spellings of the listing
tags that an earlier draft used, fall under the misspelling rule below:
`@short_desc` is one edit from `@shortdesc` and is refused, while `@when_use` is
too far from `@usewhen` to be caught and stays in the prose.

## Why `@tool` is a modifier tag

TSDoc separates _modifier_ tags, whose presence is the whole statement, from
_block_ tags, which carry content. `@public` is a modifier tag: a symbol is not
public because someone wrote `@public false`, it is simply unmarked until someone
marks it.

`@tool` is the same shape, and it kills the framing the boolean forced. With
`@tool true` / `@tool false`, absence had to _mean_ something — "absent means
false" — which is a claim about every action nobody has read. Presence-only says
the narrower true thing: an action is unmarked until its author marks it.

Exposure stays opt-in and there is still no blocklist. A new action is
unreachable by an agent until its author writes the line that reaches it, rather
than reachable until someone remembers to exclude it.

`@tool` written with a value is refused rather than read as `true`. It is almost
always the boolean this tag used to take, and a vocabulary that quietly accepts
the old spelling is one nobody finishes migrating — with `@tool false` reading as
an exposed action.

`@parallelsafe` is a modifier for the same reason. A tool either may run beside
the other parallel-safe calls next to it or may not; `@parallelsafe reads only`
qualifies a claim that has no qualified form, so a value on it is refused too.

## `tsdoc.json`, and what is standards-aligned here

TypeScript has a documentation-comment standard, and these tags are declared to
it. A space carries a `tsdoc.json` at its root, written by `simple init`:

```json
{
  "$schema": "https://developer.microsoft.com/json-schemas/tsdoc/v0/tsdoc.schema.json",
  "tagDefinitions": [
    { "tagName": "@tool", "syntaxKind": "modifier" },
    { "tagName": "@shortdesc", "syntaxKind": "block" },
    { "tagName": "@usewhen", "syntaxKind": "block", "allowMultiple": true },
    { "tagName": "@parallelsafe", "syntaxKind": "modifier" },
    { "tagName": "@Payload", "syntaxKind": "block" }
  ]
}
```

Each action carries one too, written by `simple new action`, holding nothing but
a pointer at the space's:

```json
{
  "$schema": "https://developer.microsoft.com/json-schemas/tsdoc/v0/tsdoc.schema.json",
  "extends": ["./../../../../tsdoc.json"]
}
```

**The second file is not redundant.** A TSDoc reader walks up from the source
file and stops at the first folder holding a `package.json` or a `tsconfig.json`,
then looks for `tsdoc.json` _there_ and nowhere further up. Every action holds
both of those files, so the walk always stops inside the action — and a
vocabulary kept only at the space root is never found, leaving `@tool` undefined
in every action of a space that declares it perfectly well one directory above.
Inheriting rather than restating is what keeps the vocabulary in one place
anyway; a base path that stops resolving is reported as a missing file rather
than quietly falling back to a configuration that knows none of these tags.

(A relative base path must begin with `./`, or TSDoc reads it as an NPM package
name — which is why the path above starts `./../`.)

Without the declaration, `@tool` is an _undefined_ tag: the editor underlines it
and `eslint-plugin-tsdoc` reports it. That teaches an author the annotation is a
mistake at the moment they write the one line that makes their action reachable.

The generators claim these names and **nothing else** — every other `@` line is
description, because this vocabulary shares a doc comment with `@param`,
`@remarks` and the rest of TSDoc, and a generator that lifted every tag out of
the description to be sure of catching its own would delete an author's prose to
protect itself. The editor reports `@toool` where it is typed; the build refuses
it too, because a name one edit from a claimed one was plainly meant to be it,
and an editor an author may not be running is no answer to a typo.

**Go has no equivalent standard, and this document is not implying one.** A
godoc comment is prose; the only structured conventions in Go are `//go:build`
directives and the `Deprecated:` paragraph prefix, and neither is a tag grammar.
The same spellings work in a Go action's doc comment because _this generator_
reads them there — one vocabulary, one authoring pattern, described identically
in every language — not because godoc defines them. A Go author gets
no editor recognition, and there is no file that would give them any.

```go
// Writes rows to a tenant table.
//
// @tool
// @shortdesc Write rows to one tenant table.
// @usewhen The user asks for rows to be added or corrected.
//
// @Payload Input
func handler(req simple.Request) (any, error) { ... }
```

## Where an author may write it

**Anywhere in a comment in the action's main source file.** One tag per line,
and each tag once except `@usewhen`. Position is not part of the rule: the
generators read every comment in the file, whatever it is or is not attached
to, so there is no list of legal places to learn and none to get wrong. Writing
any other tag twice fails the build rather than letting one copy win.

Where an author writes the statement must not decide whether it is heard: a
dropped `@tool` is an action that quietly stops being callable, and a dropped
qualifier is an action advertised as something it is not while the build stays
green. There used to be a list, and the list was the defect — it described what
each parser happened to reach rather than anything an author could see. A blank
line between the statement and the declaration under it detaches the comment in
Go and does nothing in TypeScript, so the same lines exposed the action in one
language and left it uncallable in the other, both at exit 0. A Rust action's
`///` and `//!` comments are read the same way.

A block does not have to end with the tags, either. An author may state them and
keep writing, and what follows is part of what the tool says it does — in the
description the artifact carries **and** in the copy of it inside the schema,
which state the same text.

## What the build writes

For the risk-register tool above, also marked `@parallelsafe`:

```json
{
  "description": "Reads the risk register for a site.",
  "schema": { "...": "..." },
  "ai": {
    "tool": true,
    "shortdesc": "Read the risk register for one site.",
    "usewhen": [
      "The user asks which risks a site carries.",
      "A plan needs the open risks before it schedules work."
    ],
    "parallelsafe": true
  }
}
```

The member order is the file format: `tool`, `shortdesc`, `usewhen`,
`parallelsafe`. `usewhen` is absent rather than empty when none is written, and
`parallelsafe` is present only when written — never `false`, because an unmarked
tool is simply not parallel-safe. The host honours it only when it is exactly
`true`. An action that is not a tool
carries no `ai` key at all — including one that writes `@shortdesc` or
`@usewhen` without `@tool`: those describe a tool to a model, an action that is
not one never enters the listing, and the lines are dropped rather than refused.

`"tool": true` is how the artifact renders the presence of a modifier tag, so a
host reading `action.json` alone sees the same statement the source makes.

## What is refused

Anything short of a complete, well-formed statement fails the build, because a
half-read annotation is how an action ends up advertised as something it is not:

- `@tool` or `@parallelsafe` carrying a value
- `@parallelsafe` without `@tool` — unlike the listing tags it is refused rather
  than dropped, because a dispatch claim on an action that is not a tool is one
  nobody acts on, and an author who wrote both and lost `@tool` is otherwise told
  nothing
- `@tool` without a `@shortdesc`, or a `@shortdesc` or `@usewhen` with nothing
  after it
- a `@shortdesc` over 300 characters, more than 10 `@usewhen` lines, or one over
  100 characters
- the same tag declared twice, except `@usewhen`
- a name one edit away from a tag above — `@toool`, `@shortdes`, `@Payloud`,
  `@parallel_safe`, `@parallelSafe` —
  which no reader would ever hear, and which the author plainly wrote to be
  heard

A refusal names the action, the tag, and what would have been accepted. It also
**discards the `action.json` already on disk** — that file was generated from an
earlier source, so it describes an action that no longer exists, and nothing
downstream can tell a stale well-formed file from a current one. A generator that
merely could not run leaves the file alone: an absent toolchain says nothing
about whether the file is true.
