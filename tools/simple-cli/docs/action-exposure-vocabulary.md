# The action exposure vocabulary

An action becomes callable by an agent because **its own source says so**. The
statement is four doc-comment tags; the build lifts them out of the doc comment
and writes them into `action.json` beside the description and the schema.

Carrying it in the source is what lets regeneration keep it. `action.json` is
rewritten wholesale on every build, so a key added to that file by hand survives
only until the next author touches the action.

## The four tags

```ts
/**
 * Reads the risk register for a site.
 *
 * @tool
 * @effects read
 * @retry safe
 * @discloses tenant_record
 */
export interface Payload { site_id: string }
```

| tag          | kind     | required                | values                                                                       |
| ------------ | -------- | ----------------------- | ---------------------------------------------------------------------------- |
| `@tool`      | modifier | —                       | none: the tag carries no value                                               |
| `@effects`   | block    | when `@tool` is present | `read` `orchestration` `write` `destructive` `external` `credential`         |
| `@retry`     | block    | when `@tool` is present | `safe` `keyed` `verify-first` `never`                                        |
| `@discloses` | block    | optional                | `tenant_record` (default) `settings_field` `credential_field` `secret_field` |

`@effects` takes one or more values, separated by commas or spaces, and declares
the **widest** thing calling the action can do — never the narrower thing its
purpose describes. An action that reaches a general-purpose bridge declares what
the bridge reaches.

`@retry` says what a caller may do with a call that failed without a verdict.
`keyed` means a repeat carrying the same idempotency key applies once;
`verify-first` means read the world before repeating; `never` means a blind
repeat is not safe at all.

`@discloses` names the narrowest class of thing the action can return. It
defaults to `tenant_record` — rows a signed-in member of the tenant could read
for themselves — so an action that returns a settings field, a stored credential
or a secret has to say so, in the same edit that makes it do so.

A tool's revision is **not** in this vocabulary. The host pins it, so there is
nothing here for an author to get wrong about it.

### A second vocabulary is in the generator and is not reachable from here

The generator this CLI embeds is carried over from the platform verbatim, and it
also describes a **Rust** vocabulary — `@tool`, `@shortdesc`, `@usewhen`. `simple
build` compiles a Rust action, but its metadata does not yet go through this
generator, so that vocabulary reaches nothing from here.

So `@shortdesc` written in a TypeScript or Go action is not an exposure tag
here, and it does not fail the build either: like any other unclaimed `@name`, it
stays where the author put it and ships as part of the description. Only a **near
miss** of a tag in the table above — one edit away from `@effects`, say — is
refused, on the grounds that it was plainly meant to be one.

It is carried anyway rather than trimmed, because half a sync is the dangerous
shape: a generator edited on the way in is one nobody can diff against the
original, and the two copies have already drifted once. Reading the whole file
and using the part that applies keeps the copy checkable.

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

## `tsdoc.json`, and what is standards-aligned here

TypeScript has a documentation-comment standard, and these tags are declared to
it. A space carries a `tsdoc.json` at its root, written by `simple init`:

```json
{
  "$schema": "https://developer.microsoft.com/json-schemas/tsdoc/v0/tsdoc.schema.json",
  "tagDefinitions": [
    { "tagName": "@tool", "syntaxKind": "modifier" },
    { "tagName": "@effects", "syntaxKind": "block" },
    { "tagName": "@retry", "syntaxKind": "block" },
    { "tagName": "@discloses", "syntaxKind": "block" }
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

The declaration is also the only thing that catches a misspelling. The generators
claim these four names and **nothing else** — every other `@` line is
description, because this vocabulary shares a doc comment with `@param`,
`@remarks` and the rest of TSDoc, and a generator that lifted every tag out of
the description to be sure of catching its own would delete an author's prose to
protect itself. So `@toool` is reported where it is typed, by the editor, rather
than read as a sentence by the build.

**Go has no equivalent standard, and this document is not implying one.** A
godoc comment is prose; the only structured conventions in Go are `//go:build`
directives and the `Deprecated:` paragraph prefix, and neither is a tag grammar.
The same four spellings work in a Go action's doc comment because _this
generator_ reads them there — one vocabulary, one authoring pattern, described
identically by both generators — not because godoc defines them. A Go author gets
no editor recognition, and there is no file that would give them any.

```go
// Writes rows to a tenant table.
//
// @tool
// @effects write, destructive
// @retry never
//
// @Payload Input
func handler(req simple.Request) (any, error) { ... }
```

## Where an author may write it

**Anywhere in a comment in the action's main source file, once.** One tag per
line. Position is not part of the rule: both generators read every comment in
the file, whatever it is or is not attached to, so there is no list of legal
places to learn and none to get wrong. Writing the same tag twice fails the
build rather than letting one copy win.

Where an author writes the statement must not decide whether it is heard: a
dropped `@tool` is an action that quietly stops being callable, and a dropped
qualifier is an action advertised as something it is not while the build stays
green. There used to be a list, and the list was the defect — it described what
each parser happened to reach rather than anything an author could see. A blank
line between the statement and the declaration under it detaches the comment in
Go and does nothing in TypeScript, so the same four lines exposed the action in
one language and left it uncallable in the other, both at exit 0.

A block does not have to end with the tags, either. An author may state them and
keep writing, and what follows is part of what the tool says it does — in the
description the artifact carries **and** in the copy of it inside the schema,
which state the same text.

## What the build writes

```json
{
  "description": "Reads the risk register for a site.",
  "schema": { "...": "..." },
  "ai": {
    "tool": true,
    "effects": ["read"],
    "retry": "safe",
    "discloses": "tenant_record"
  }
}
```

An action that declares nothing carries no `ai` key at all.

`"tool": true` is how the artifact renders the presence of a modifier tag, so a
host reading `action.json` alone sees the same statement the source makes.

## What is refused

Anything short of a complete, well-formed statement fails the build, because a
half-read annotation is how an action ends up advertised as something it is not:

- `@tool` carrying a value
- `@effects` or `@retry` missing while `@tool` is present
- `@effects`, `@retry` or `@discloses` written without `@tool`
- a value outside the lists above, or an effect named twice
- the same tag declared twice
- a name one edit away from a tag above — `@effect`, `@disclose` — which no
  reader would ever hear, and which the author plainly wrote to be heard

A refusal names the action, the tag, and what would have been accepted. It also
**discards the `action.json` already on disk** — that file was generated from an
earlier source, so it describes an action that no longer exists, and nothing
downstream can tell a stale well-formed file from a current one. A generator that
merely could not run leaves the file alone: an absent toolchain says nothing
about whether the file is true.
