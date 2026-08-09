/* eslint-disable node/prefer-global/process */
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { BasicAnnotationsReader, createGenerator } from 'ts-json-schema-generator'
import { Project, SyntaxKind, ts } from 'ts-morph'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// THE AUTHOR-FACING EXPOSURE VOCABULARY.
//
// An action becomes callable by an agent because its own source says so, one
// tag per line, anywhere in a comment in the action's main file, once. Carrying
// it in the source is what lets regeneration keep it: this file rewrites
// action.json wholesale, so anything added to that file by hand is deleted the
// next time an author touches the action.
//
// Exposure is opt-in and there is no blocklist. An action that declares nothing
// is not a tool, so a new action is unreachable by an agent until its author
// writes the sentence that reaches it — rather than reachable until someone
// remembers to exclude it.
//
// `@tool` IS A MODIFIER TAG in the TSDoc sense: it carries no value, and its
// presence is the whole statement. A tag that took a boolean made absence mean a
// default — an action was not a tool because nobody said `false` — and a default
// is a claim about actions nobody has read. Presence-only says the narrower true
// thing: an action is unmarked until its author marks it, the way a symbol is
// not `@public` until it says so.
//
// Only these four names are claimed as annotations. Every other `@` line is
// description, because this vocabulary shares a doc comment with `@param`,
// `@remarks` and the rest of TSDoc, and a generator that lifted every tag out of
// the description would delete an author's prose to protect its own.
//
// A NAME ONE EDIT AWAY FROM A CLAIMED ONE IS REFUSED RATHER THAN LEFT AS PROSE.
// tsdoc.json declares these four to the editor and to ESLint, and that was taken
// here as the answer to a misspelling. It is not one: it reports `@toool` in an
// editor an author may not be running, it says nothing at build time, and the
// other generator reads Go doc comments that tsdoc.json does not reach at all —
// so the same typo stopped nothing in either language. What it costs is silent
// twice over: the class `@dicloses secret_field` was written to tighten falls
// back to the loosest one, and the line itself travels into the description a
// model reads.
//
// The host, not the author, pins a tool's revision: it is not in this
// vocabulary and there is nothing here for an author to get wrong about it.
const TOOL_TAG = 'tool'

// THE TAGS THE SCHEMA GENERATOR TURNS INTO A CONSTRAINT, ASKED OF IT RATHER
// THAN LISTED HERE.
//
// A payload member's doc comment carries two vocabularies. The exposure
// statement above is one; `@minimum`, `@maxLength`, `@asType` and the rest are
// the other, and the schema generator reads them into the constraints beside
// the description. Neither is prose: a description that kept them ships
// `@maximum 500` to a model as a sentence about what the member means.
//
// Read off the generator's own reader, so a tag it starts claiming stops being
// prose in the same release rather than the next time somebody notices a
// constraint advertised twice — once as a keyword and once as English. Its
// extended reader takes three more from the symbol directly and they belong to
// no such set, so those are the only names written out.
//
// The Go extractor claims none of this, and that is not two vocabularies
// wearing one name: a Go action states its constraints in the struct tag, where
// a doc comment never sees them.
const SCHEMA_TAGS = new Set([
  'asType',
  'example',
  'nullable',
  ...BasicAnnotationsReader.jsonTags,
  ...BasicAnnotationsReader.textTags,
])

// `@description` IS THE ONE NAME IN THAT SET THIS FILE MUST NOT REMOVE.
//
// Every other name reaches the artifact as a constraint the schema generator
// wrote — `@maximum 500` leaves the prose and arrives as `maximum`, so removing
// the line moves the meaning rather than losing it. This one arrives as the
// description, which the walk above then OVERWRITES with the prose that was
// kept; removing the line therefore deletes the author's sentence outright and
// the artifact carries neither the tag nor the text. Measured: an action
// documented only by `@description` shipped an empty description, and a member
// documented only by one shipped no description key at all, from a build that
// exited zero.
SCHEMA_TAGS.delete('description')

// THE VOCABULARY A RUST ACTION IS AUTHORED IN.
//
// Three tags on the action and nothing else. `@tool` is the same modifier tag
// the vocabulary above claims — bare, presence-only, and refused if a value
// follows it. The other two are written for the MODEL rather than for the host:
// `@short_desc` is the one line a tool listing shows, and `@when_use` is
// repeatable and says when to reach for it. The doc comment's own prose is not
// in this list and is not touched: it is the full contract, and it arrives when
// the tool is selected rather than in the listing.
//
// `@Payload` is claimed too, though it states nothing about exposure: it names
// the struct the schema is read from, so an author may point at a type not
// called `Payload`. It is claimed for the same reason the three are — this
// generator READS it, and a directive to this generator that stayed in the
// prose would be shipped to a model as a sentence about what the action does.
// Claiming it also puts `@Payloud` in front of the misspelling rule below,
// where the alternative is a silent fall back to a struct of the other name and
// a schema describing the wrong type.
//
// WHAT CALLING A TOOL DOES IS NOT IN THIS VOCABULARY. `@effects`, `@retry` and
// `@discloses` are facts the host states in its own table, about a tool it
// already knows. They are simply not claimed here, so a line writing one is
// prose like any other unclaimed `@name`.
const SHORTDESC_TAG = 'shortdesc'
const USEWHEN_TAG = 'usewhen'
const PAYLOAD_TAG = 'Payload'

const ACTION_TAGS = [TOOL_TAG, SHORTDESC_TAG, USEWHEN_TAG, PAYLOAD_TAG]

// The tags that qualify `@tool` in Rust. Either one written without it is a
// statement about nothing, the same way the three above are.
const QUALIFYING_TAGS = [SHORTDESC_TAG, USEWHEN_TAG]

// A LISTING HAS TO STAY SMALL, SO WHAT DOES NOT FIT IS REFUSED, NEVER DROPPED.
//
// Every `@short_desc` and `@when_use` an action writes is carried into the
// listing an agent chooses from, and that listing is read in full on every turn.
// Keeping the first few and discarding the rest would be a cap nobody was told
// about: the author reads the line in the source, the model never sees it, and
// the build that decided so exited zero.
//
// The widths are the ruled ones. A tool costs a few thousand bytes once it is
// chosen; what these bound is the entry that is carried whether it is chosen or
// not, which is the number that multiplies by the size of the catalogue.
const USEWHEN_LIMIT = 10
const SHORTDESC_CHARS = 300
const USEWHEN_CHARS = 100

// The status a refused exposure statement exits with, told apart from every
// other way this generator can fail. A caller reading only "non-zero" cannot
// distinguish a source the vocabulary refuses from a toolchain that is not
// installed, and the two call for opposite things to happen to the action.json
// already on disk.
const ANNOTATION_REFUSAL_EXIT_CODE = 2

function annotationError(action, message, accepted) {
  const suffix = accepted ? ` Accepted: ${accepted.join(', ')}.` : ''
  return new Error(`${action}: ${message}.${suffix}`)
}

// EVERY DESCRIPTION IN THE ARTIFACT IS READ ONCE, FROM THE SOURCE, BY THIS
// FILE.
//
// Two parsers used to read the same doc comments on the way to one file.
// `splitDoc` below lifts the annotations out line by line and keeps everything
// else; the schema generator opens the source itself and asks TypeScript, and
// TypeScript ENDS a doc comment at its first tag. Both answers shipped — the
// action's own description from one reader, every description inside the schema
// from the other — so an author who wrote a sentence after a tag had it kept in
// one place and deleted in the other, from a single run that exited zero with a
// well-formed file and no reader able to tell which half its author wrote.
//
// A description cut at a tag is worse than a missing one, because the cut lands
// mid-sentence and the surviving half reads as a complete claim: an action that
// says its script is "evaluated with only the language's computational builtins
// in scope" — with the clause naming what is ABSENT deleted — advertises the
// opposite of the rule its author wrote.
//
// So the schema generator is no longer asked what anything MEANS. It
// contributes the shape and the constraints; every sentence the walk below
// reaches is stated from the source or removed, and two readings have nothing
// left to disagree about. Reconciling them instead left two parsers to keep
// agreeing, and deciding which of them wins is what shipped the wrong block.
//
// The catalog already replaces a schema description with the action's own
// before advertising it to a model. This is the same rule one layer earlier, so
// every consumer of action.json sees it rather than only the tools that catalog
// reaches — and a third-party action, which never passes through it, is
// described by the sentences its author wrote.
//
// The walk follows what a payload IS: an object's members, and an array's
// elements. A node reached only through a union's branches is described by the
// TYPE that branch names rather than by any member of the payload, and it is
// left as the schema generator rendered it — the one description in the
// artifact this file did not read.
function applySourceDescriptions(schema, description, type) {
  if (!schema || typeof schema !== 'object' || Array.isArray(schema)) {
    return
  }

  // A schema either states a description or carries none. An empty string is a
  // third thing, and it reads as a statement to every consumer that checks
  // whether the key is there.
  if (description) {
    schema.description = description
  }
  else {
    delete schema.description
  }

  if (!type) {
    return
  }

  const properties = schema.properties

  if (properties && typeof properties === 'object') {
    for (const [name, propertySchema] of Object.entries(properties)) {
      const symbol = type.getProperty(name)
      const declaration = symbol && declarationOf(symbol)

      if (declaration) {
        applySourceDescriptions(
          propertySchema,
          describedBy(declaration),
          symbol.getTypeAtLocation(declaration),
        )
      }
    }
  }

  const elementType = type.getArrayElementType()

  if (schema.items && elementType) {
    applySourceDescriptions(schema.items, describedBy(declarationOfType(elementType)), elementType)
  }
}

// The exposure statement a RUST action makes about itself, or nothing at all.
//
// THE VOCABULARY IS STATED HERE AND NOWHERE ELSE, for the same reason the
// TypeScript one is: this generator writes action.json, and a rule about what
// may appear in that file that lives in a second program is a rule two
// languages get to disagree about. The Go extractor already holds a second copy
// of the vocabulary above, in Go, and keeping those two agreeing is work that
// buys nothing. The Rust companion parses Rust — comments, types, serde
// attributes — and states no opinion about which tags are claimed, what they
// mean, or when a source is refused.
//
// Shaped after `buildAiMetadata` and refusing in the same order for the same
// reasons: a mistyped name first, because it explains a missing tag; then a
// qualifier without `@tool`; then a value on the modifier tag. The wording is
// repeated rather than shared, because sharing it would have meant editing the
// function the other two languages are already refused by.
function buildAiMetadata(action, tags, misspellings) {
  const accepted = ACTION_TAGS.map(tag => `@${tag}`)
  const [refusal] = misspellings

  if (refusal) {
    throw annotationError(
      action,
      `writes @${refusal.written}, which nothing claims and which is one edit from @${refusal.meant}`,
      accepted,
    )
  }

  // `@Payload` says which type the schema was read from, which the companion
  // has already acted on. It is not part of the statement about exposure, and
  // an action that writes it and nothing else is not making one.
  const stated = tags.filter(tag => tag.name !== PAYLOAD_TAG)

  if (stated.length === 0) {
    return undefined
  }

  const present = new Set(stated.map(tag => tag.name))
  const whenUse = stated.filter(tag => tag.name === USEWHEN_TAG).map(tag => tag.value)
  const declared = new Map()

  // `@when_use` is the one repeatable name, so it is collected above rather
  // than refused here. Everything else may be written once: a second
  // `@short_desc` is two answers to one question, and picking either is
  // deciding on the author's behalf which sentence they meant.
  for (const tag of stated) {
    if (tag.name === USEWHEN_TAG) {
      continue
    }

    if (declared.has(tag.name)) {
      throw annotationError(action, `@${tag.name} is declared more than once`)
    }

    declared.set(tag.name, tag.value)
  }

  if (!declared.has(TOOL_TAG)) {
    // Named in vocabulary order rather than in the order they were declared, so
    // the same source is refused with the same sentence every time.
    const written = QUALIFYING_TAGS.filter(tag => present.has(tag)).map(tag => `@${tag}`)

    throw annotationError(
      action,
      `declares ${written.join(', ')} without @${TOOL_TAG}, so it is not a tool and the rest says nothing`,
    )
  }

  // A modifier tag is its own statement. A value written after one is an author
  // saying something the vocabulary has no way to hear — most likely the boolean
  // this tag used to take, whose `false` no longer says anything.
  const value = declared.get(TOOL_TAG)

  if (value !== '') {
    throw annotationError(
      action,
      `@${TOOL_TAG} is a modifier tag and takes no value, and this one carries "${value}". `
      + 'Leave it bare to expose the action, or delete it to leave the action unexposed',
    )
  }

  // Required, and with no default to fall back to. The listing an agent chooses
  // from carries this line and the prose arrives only after it has chosen, so a
  // tool without one is offered as a name and nothing else — and a default
  // written here would be this generator describing an action it has not read.
  if (!declared.has(SHORTDESC_TAG)) {
    throw annotationError(action, `is a tool and must declare @${SHORTDESC_TAG}`)
  }

  const shortDesc = declared.get(SHORTDESC_TAG)

  if (shortDesc === '') {
    throw annotationError(action, `@${SHORTDESC_TAG} is written with nothing after it`)
  }

  if (whenUse.includes('')) {
    throw annotationError(action, `@${USEWHEN_TAG} is written with nothing after it`)
  }

  if (shortDesc.length > SHORTDESC_CHARS) {
    throw annotationError(
      action,
      `writes a @${SHORTDESC_TAG} of ${shortDesc.length} characters and a listing carries at `
      + `most ${SHORTDESC_CHARS}. Say the rest in the prose, which is read once the tool is `
      + 'chosen',
    )
  }

  if (whenUse.length > USEWHEN_LIMIT) {
    throw annotationError(
      action,
      `declares ${whenUse.length} @${USEWHEN_TAG} lines and a listing carries at most `
      + `${USEWHEN_LIMIT}. Say the rest in the prose, which is read once the tool is chosen`,
    )
  }

  const overlong = whenUse.find(line => line.length > USEWHEN_CHARS)

  if (overlong !== undefined) {
    throw annotationError(
      action,
      `writes a @${USEWHEN_TAG} of ${overlong.length} characters and each carries at most `
      + `${USEWHEN_CHARS}. A trigger is one line; the prose holds what it does`,
    )
  }

  // The member order below is the file format rather than a style choice.
  /* eslint-disable perfectionist/sort-objects */
  const ai = { tool: true, shortdesc: shortDesc }
  /* eslint-enable perfectionist/sort-objects */

  // Absent rather than empty when the author wrote none, so a reader is never
  // handed an empty list to tell apart from an unstated one.
  if (whenUse.length > 0) {
    ai.usewhen = whenUse
  }

  return ai
}

// Every comment in a source file, with the syntax that makes it a comment taken
// off and nothing else touched.
//
// THE SCANNER IS ASKED RATHER THAN THE SYNTAX TREE. A `/** */` block is a node
// the tree hands back; an ordinary `//` line is trivia and is not. So a walk
// over the tree heard the first and was silent on the second, and the same four
// lines exposed the action or did not depending on which comment syntax their
// author reached for — while the other generator, which reads every comment its
// parser found, heard both. One vocabulary answering differently in two
// languages is two vocabularies wearing one name.
//
// Only the four names are claimed and each may be written once, so reading more
// comments cannot make an author's prose mean something: a line either is one of
// the four or is left exactly where it was written.
function commentsIn(sourceText) {
  const scanner = ts.createScanner(ts.ScriptTarget.Latest, false)
  const comments = []

  scanner.setText(sourceText)

  for (let token = scanner.scan(); token !== ts.SyntaxKind.EndOfFileToken; token = scanner.scan()) {
    if (
      token === ts.SyntaxKind.SingleLineCommentTrivia
      || token === ts.SyntaxKind.MultiLineCommentTrivia
    ) {
      comments.push(withoutCommentMarkers(scanner.getTokenText()))
    }
  }

  return comments
}

// The declaration a symbol's doc comment is written on.
//
// Almost always the only one. A symbol declared more than once is documented by
// whichever of them an author wrote the comment on, so the search runs backwards
// and stops at the first documented declaration rather than assuming a position.
function declarationOf(symbol) {
  const declarations = symbol.getDeclarations()

  return declarations.findLast(declaration => docBlockOf(declaration))
    ?? declarations[declarations.length - 1]
}

// The declaration a TYPE is documented on, for the schema nodes that describe a
// type rather than a member of one — an array's elements are the case that
// exists.
function declarationOfType(type) {
  const symbol = type.getAliasSymbol() ?? type.getSymbol()

  return symbol ? declarationOf(symbol) : undefined
}

// The description a declaration's own doc block states, with the annotations
// lifted out of it.
function describedBy(node) {
  const docBlock = docBlockOf(node)

  return docBlock ? splitDoc(docBlock.getInnerText()).description : ''
}

// THE DOC BLOCK A DECLARATION STATES IS THE ONE WRITTEN AGAINST IT.
//
// Where an author leaves two blocks stacked above one declaration, TypeScript
// attributes the LOWER one: it is what an editor shows on hover and what the
// schema generator reads. Taking the first described the action by whichever
// block was written earliest — most often the one the author had just replaced,
// and always the opposite of what they were looking at.
function docBlockOf(node) {
  if (!node || typeof node.getJsDocs !== 'function') {
    return undefined
  }

  const blocks = node.getJsDocs()

  return blocks[blocks.length - 1]
}

function inlineRootDefinition(schema) {
  if (
    schema
    && typeof schema === 'object'
    && !Array.isArray(schema)
    && typeof schema.$ref === 'string'
    && schema.$ref.startsWith('#/definitions/')
    && schema.definitions
    && typeof schema.definitions === 'object'
  ) {
    const definitionName = schema.$ref.replace('#/definitions/', '')
    const definition = schema.definitions[definitionName]

    if (definition && typeof definition === 'object' && !Array.isArray(definition)) {
      const inlined = { ...definition }
      delete inlined.definitions
      return inlined
    }
  }

  return schema
}

function noInputSchema() {
  return {
    additionalProperties: false,
    properties: {},
    type: 'object',
  }
}

function normalizeGeneratedSchema(schema) {
  if (!schema || typeof schema !== 'object' || Array.isArray(schema)) {
    return noInputSchema()
  }

  schema = inlineRootDefinition(schema)

  if (Object.keys(schema).length === 0) {
    return noInputSchema()
  }

  normalizeOpenDictionarySchemas(schema)
  return schema
}

function normalizeOpenDictionarySchemas(node) {
  if (!node || typeof node !== 'object') {
    return
  }

  if (Array.isArray(node)) {
    node.forEach(normalizeOpenDictionarySchemas)
    return
  }

  if (
    Object.prototype.hasOwnProperty.call(node, 'additionalProperties')
    && node.additionalProperties
    && typeof node.additionalProperties === 'object'
    && !Array.isArray(node.additionalProperties)
    && Object.keys(node.additionalProperties).length === 0
  ) {
    node.additionalProperties = true
  }

  Object.values(node).forEach(normalizeOpenDictionarySchemas)
}
// A REFUSED SOURCE TAKES ITS STALE OUTPUT WITH IT.
//
// action.json is generated wholesale from the source beside it. When the source
// is refused, the file still sitting there was generated from an EARLIER
// source: it describes an action that no longer exists, and it carries an
// exposure statement its author has since tried to change. Nothing downstream
// can tell — a well-formed file reads as current — so the refused edit ships as
// though it had been accepted while the author is told the build failed.
//
// Removed only for a refusal. A generator that could not run says nothing about
// whether the file is true, and deleting every action's metadata because a
// toolchain is absent turns one environment problem into a tree nobody can
// build from.
function refuse(actionDir) {
  fs.rmSync(path.join(actionDir, 'action.json'), { force: true })
  process.exit(ANNOTATION_REFUSAL_EXIT_CODE)
}

// What the Rust companion read out of an action's source, in four members and
// no fifth: `description`, the doc comment that describes the action exactly as
// its author wrote it; `schema`, read off the payload type; `comments`, every
// comment in the file, which is where the exposure statement is read from; and
// `gaps`, the things the schema had no way to state.
//
// THE COMPANION IS BUILT AND THEN RUN, RATHER THAN RUN THROUGH `cargo run`, for
// the reason written against the Go branch below: `cargo run` is a launcher,
// and a launcher's status describes the launcher. A refusal that arrives as an
// ordinary failure is a refusal this process cannot act on, and the action.json
// generated from an earlier source then survives the edit that was refused —
// well-formed, current-looking, and describing an action that no longer exists.
//
// It does NOT build into a throwaway directory the way the Go branch does. `go
// build` keeps its own cache elsewhere, so discarding the output directory
// costs a link; cargo keeps everything in the one it is given, so a fresh
// directory per action rebuilds every dependency for every action in the tree.
// The companion's own directory is named instead — the one its `.gitignore`
// already excludes — so the cache is the same one next time and the same one
// anybody running `cargo` in that crate has already warmed. Two builds racing
// it is cargo's own lock, not a correctness question.
//
// Named rather than left to cargo's default, which is the same directory until
// something in the environment redirects it. The binary is then read from a
// path this file chose rather than one it assumed, so a redirected build cannot
// present as a build that produced nothing.
//
// The companion parses Rust and answers with what it found. It states nothing
// about the vocabulary: the tags are claimed, validated and refused in this
// file, so `ai` arriving from it is not a value to merge but a sign that the
// rule now has two homes, and it fails the run saying so.
function rustCompanionOutput(actionDir, rustPath) {
  const cratePath = path.join(__dirname, 'extract_rustdoc')
  const manifestPath = path.join(cratePath, 'Cargo.toml')
  const buildDir = path.join(cratePath, 'target')
  // NEITHER OF THESE IS AN AUTHOR'S MISTAKE, so neither discards the file on
  // disk: a companion that could not be built, and one that answered with
  // something this generator cannot read, have both said nothing about whether
  // the action's own source is well-formed. They are named apart from each
  // other because the fix is in a different place — a toolchain in one case and
  // the companion's own output in the other — and a heading naming the build
  // sends a reader to look at a build that succeeded.
  const buildFailure = (message, detail) => {
    console.error(`Failed to build the Rust metadata extractor for ${actionDir}: ${message}`)

    if (detail) {
      console.error(detail)
    }

    process.exit(1)
  }

  const answerFailure = (message) => {
    console.error(`The Rust metadata extractor answered ${actionDir} with ${message}`)
    process.exit(1)
  }

  if (!fs.existsSync(manifestPath)) {
    buildFailure(`no manifest at ${manifestPath}`)
  }

  try {
    execFileSync('cargo', ['build', '--quiet', '--manifest-path', manifestPath, '--target-dir', buildDir])
  }
  catch (err) {
    buildFailure(err.message, err.stderr && err.stderr.toString())
  }

  // Built without `--release`: this reads one source file per action and the
  // time that matters is the compile, not the run.
  const extractorPath = path.join(buildDir, 'debug', 'extract_rustdoc')

  if (!fs.existsSync(extractorPath)) {
    buildFailure(`the build produced no binary at ${extractorPath}`)
  }

  let data
  try {
    data = JSON.parse(execFileSync(extractorPath, ['--', rustPath]).toString())
  }
  catch (err) {
    // A Rust action that cannot be described must fail the run, not leave the
    // stale action.json in place while the process exits cleanly. A refusal
    // reads the same to this generator's caller whichever language the action
    // is written in, and only a refusal discards the stale file. The
    // companion's own message has already reached this process's stderr, so it
    // is not restated under a heading that would make an author's mistake read
    // as a broken toolchain.
    if (err.status === ANNOTATION_REFUSAL_EXIT_CODE) {
      refuse(actionDir)
    }

    console.error(`Failed to extract the Rust doc comments for ${actionDir}:`, err.message)
    if (err.stdout)
      console.error(err.stdout.toString())
    if (err.stderr)
      console.error(err.stderr.toString())
    process.exit(1)
  }

  if (!data || typeof data !== 'object' || Array.isArray(data)) {
    answerFailure('something that is not an object')
  }

  // WITHOUT THE COMMENTS THERE IS NO STATEMENT TO READ, and an action whose
  // author wrote `@tool` would regenerate as an action that is not a tool —
  // from a run that exited zero. That is the failure this annotation exists to
  // make impossible, so an answer missing them fails the run rather than being
  // read as a file with nothing in it.
  if (!Array.isArray(data.comments)) {
    answerFailure(
      'no `comments` array, which is where the exposure statement is read from',
    )
  }

  if (data.ai !== undefined) {
    answerFailure(
      'an `ai` object. The tag vocabulary is claimed, validated and refused in this file, '
      + 'and a second copy of it is how two languages drift apart',
    )
  }

  // A member this generator has no vocabulary to describe — the constraints a
  // TypeScript action writes in a doc comment and a Go action in a struct tag,
  // which Rust has neither of. Reported where the author can see it, and not
  // guessed at: an invented spelling would be a rule nobody ruled on, advertised
  // to every action written after it.
  for (const gap of Array.isArray(data.gaps) ? data.gaps : []) {
    console.error(`${path.basename(actionDir)}: ${gap}`)
  }

  return data
}

// The description a comment states, and the exposure annotations written inside
// it.
//
// The annotations are LIFTED OUT of the description line by line, and the
// description is everything else. A comment does not have to end with them: an
// author may state the tags and keep writing, and what follows is part of what
// the tool says it does.
//
// Read as a tail instead — the text before the first tag — that trailing
// sentence is not merely misplaced, it is DELETED. A TypeScript parser hands
// every line after a tag back as that tag's own comment, so the description
// silently loses the paragraph while the build stays green and the annotation
// still parses. The rule an author wrote down then never reaches the model,
// which is worse than an unparsed one, because nothing failed.
//
// This is the same line-wise rule the Go extractor applies, so one authoring
// pattern is described identically by both generators.
//
// Two vocabularies are claimed and only one is returned. The exposure tags are
// this file's own and are what a caller asks for; the schema generator's tags
// are removed because it has already turned them into CONSTRAINTS, and a
// constraint stated twice — once as a keyword and once as English — is a member
// documented by whichever one the reader believes. `@description` is the
// exception and is kept, because it is the one of those that arrives as prose
// this file then replaces rather than as a constraint that survives.
//
// Everything else is what the author wrote: a `@param` or a `@remarks` stays
// where they put it.
function splitDoc(text) {
  const descriptionLines = []
  const misspellings = []
  const tags = []

  for (const line of String(text).split('\n')) {
    const trimmed = line.trim()
    const name = trimmed.startsWith('@') ? trimmed.slice(1).split(/\s+/)[0] : ''

    if (ACTION_TAGS.includes(name)) {
      tags.push({ name, value: trimmed.slice(name.length + 1).trim() })
      continue
    }

    if (SCHEMA_TAGS.has(name)) {
      continue
    }

    // A near miss is left in the description rather than lifted out of it,
    // because it is refused before any description ships.
    const meant = name && ACTION_TAGS.find(claimed => withinOneEdit(name, claimed))

    if (meant) {
      misspellings.push({ meant, written: name })
    }

    descriptionLines.push(line)
  }

  return { description: descriptionLines.join('\n').trim(), misspellings, tags }
}

// The same line-wise reading, for the vocabulary a Rust action is written in.
//
// The claimed names are lifted out and everything else is left exactly where
// the author put it, so a comment may state its tags and keep writing. One
// difference from the reading above, and it is the whole reason this is a
// separate function rather than a parameter: THE SCHEMA GENERATOR'S TAGS ARE
// NOT REMOVED. `@minimum` and `@pattern` leave a TypeScript doc comment because
// a generator has already turned them into constraints beside the description,
// and a constraint stated twice is a member documented by whichever copy the
// reader believes. Nothing does that for Rust — there is no constraint
// vocabulary for a Rust member and none has been ruled on — so removing those
// lines here would delete the author's sentence and put nothing in its place.
//
// A retired name is recorded rather than lifted, and the line stays in the
// description, because it is refused before any description ships.
function splitRustDoc(text) {
  const descriptionLines = []
  const misspellings = []
  const tags = []

  for (const line of String(text).split('\n')) {
    const trimmed = line.trim()
    const name = trimmed.startsWith('@') ? trimmed.slice(1).split(/\s+/)[0] : ''

    if (ACTION_TAGS.includes(name)) {
      tags.push({ name, value: trimmed.slice(name.length + 1).trim() })
      continue
    }

    if (name) {
      const meant = ACTION_TAGS.find(claimed => withinOneEdit(name, claimed))

      if (meant) {
        misspellings.push({ meant, written: name })
      }
    }

    descriptionLines.push(line)
  }

  return { description: descriptionLines.join('\n').trim(), misspellings, tags }
}

// EVERY DESCRIPTION A RUST ACTION'S SCHEMA CARRIES, SPLIT BY THE ONE READER
// THAT OWNS THE VOCABULARY.
//
// The companion answers with what each author wrote and nothing lifted out,
// because the tags are claimed here. That is one rule with one home, and a rule
// with one home has to be applied everywhere a description reaches the
// artifact — not only to the action's own. A payload member documented with a
// claimed name would otherwise ship that line to a model as a sentence about
// what the member means, from a run that exited zero, while the action's own
// description beside it had the same line taken out. Two descriptions of one
// action that disagree leave no reader able to tell which one its author wrote.
//
// A description is stated or absent. An empty string is a third thing, and it
// reads as a statement to every consumer that checks whether the key is there,
// so a description that was nothing but claimed lines loses the key rather than
// keeping an empty one.
function stripRustAnnotations(node) {
  if (!node || typeof node !== 'object') {
    return
  }

  if (Array.isArray(node)) {
    node.forEach(stripRustAnnotations)
    return
  }

  if (typeof node.description === 'string') {
    const { description } = splitRustDoc(node.description)

    if (description) {
      node.description = description
    }
    else {
      delete node.description
    }
  }

  Object.values(node).forEach(stripRustAnnotations)
}

// Whether one name reaches the other by inserting, deleting or replacing a
// single character.
//
// One edit is the distance a typo travels. Any more and a name stops being a
// misspelling of this vocabulary and starts being somebody else's word, which
// this generator has no business refusing — a doc comment here is shared with
// TSDoc and with whatever else reads it.
function withinOneEdit(written, claimed) {
  if (written === claimed) {
    return true
  }

  const [longer, shorter] = written.length >= claimed.length ? [written, claimed] : [claimed, written]

  if (longer.length - shorter.length > 1) {
    return false
  }

  for (let index = 0; index < shorter.length; index++) {
    if (longer[index] === shorter[index]) {
      continue
    }

    return longer.length === shorter.length
      // A replacement: the rest must match exactly.
      ? longer.slice(index + 1) === shorter.slice(index + 1)
      // An insertion in the longer name: the rest must match what is left of
      // the shorter one.
      : longer.slice(index + 1) === shorter.slice(index)
  }

  // Every character matched up to the shorter name's end, so the longer name is
  // at most one character further on.
  return true
}

// One comment's text: what its author wrote, with the leading `//`, the
// enclosing `/* */` and the `*` that conventionally starts each line of a block
// removed, so a tag reads the same whichever syntax carries it.
function withoutCommentMarkers(comment) {
  return comment
    .replace(/^\/{2,}/, '')
    .replace(/^\/\*+/, '')
    .replace(/\*+\/$/, '')
    .split('\n')
    .map(line => line.replace(/^\s*\*+ ?/, ''))
    .join('\n')
}

let actionDir = process.argv[2]
if (!actionDir) {
  console.error('Usage: node extract-action-metadata.js <action_dir>')
  process.exit(1)
}

// If it's not absolute, resolve it relative to process.cwd()
if (!path.isAbsolute(actionDir)) {
  actionDir = path.resolve(process.cwd(), actionDir)
}

const tsPath = [
  path.join(actionDir, 'index.ts'),
  path.join(actionDir, 'src', 'index.ts'),
].find(candidate => fs.existsSync(candidate))
const goPath = path.join(actionDir, 'main.go')

// A RUST ACTION IS A CRATE, AND ITS MAIN FILE IS WHERE CARGO LOOKS FOR ONE.
//
// `src/main.rs` first, because that is the layout cargo builds without being
// told anything: a crate whose manifest names no path has its binary there, and
// every action generated from a template will have it there. `main.rs` beside
// the manifest is accepted after it, for the hand-written crate that sets
// `path` — the same shape the TypeScript branch accepts `index.ts` in, and
// cheaper to honour than to explain.
//
// The branch is reached only after Go, so no action that builds today changes
// language: a directory holding both a `main.go` and a `main.rs` is the Go
// action it was yesterday, and nothing already in the tree can be re-read by
// the new path.
const rustPath = [
  path.join(actionDir, 'src', 'main.rs'),
  path.join(actionDir, 'main.rs'),
].find(candidate => fs.existsSync(candidate))

if (tsPath) {
  const project = new Project()
  const sourceFile = project.addSourceFileAtPath(tsPath)

  // The two blocks an author may describe an action in: the Payload interface,
  // and the handler when the interface says nothing.
  const payloadInterface = sourceFile.getInterface('Payload')

  const handlerFunc = sourceFile.getFunction('handler') || sourceFile.getVariableDeclaration('handler')
  const handlerNode = handlerFunc && handlerFunc.getKindName() === 'VariableDeclaration'
    ? handlerFunc.getFirstAncestorByKind(SyntaxKind.VariableStatement)
    : handlerFunc

  const description = describedBy(payloadInterface) || describedBy(handlerNode)

  // EVERY comment in the file is read for exposure annotations, not only the
  // one that supplied the description.
  //
  // Which comment describes the action is decided above, by rules about where a
  // payload is declared. Where an author writes the exposure statement must not
  // be decided by those rules as a side effect: a tag written in a comment that
  // did not win would be dropped in silence, and a dropped `@tool` is an action
  // that quietly stops being callable — the failure this whole annotation
  // exists to make impossible.
  const comments = commentsIn(sourceFile.getFullText()).map(splitDoc)

  const exposureTags = comments.flatMap(comment => comment.tags)
  const misspellings = comments.flatMap(comment => comment.misspellings)

  let ai
  try {
    ai = buildAiMetadata(path.basename(actionDir), exposureTags, misspellings)
  }
  catch (err) {
    console.error(err.message)
    refuse(actionDir)
  }

  // Generate schema
  let schema = noInputSchema()
  if (payloadInterface) {
    try {
      const config = {
        path: tsPath,
        skipTypeCheck: true,
        tsconfig: path.join(actionDir, 'tsconfig.json'),
        type: 'Payload',
      }

      if (!fs.existsSync(config.tsconfig)) {
        delete config.tsconfig
      }

      const generator = createGenerator(config)
      schema = generator.createSchema(config.type)
      delete schema.$schema
      schema = normalizeGeneratedSchema(schema)
      applySourceDescriptions(schema, description, payloadInterface.getType())
    }
    catch (err) {
      // A missing root type means the action declares no Payload, which is a
      // valid no-input action; anything else is a real failure to describe an
      // action, and the process must exit non-zero rather than write a
      // degraded schema that a caller would trust.
      if (err.message && !err.message.includes('No root type')) {
        console.error(`Failed to generate schema for ${actionDir}:`, err)
        process.exit(1)
      }
    }
  }

  const out = {
    description,
    schema,
  }

  if (ai) {
    out.ai = ai
  }

  fs.writeFileSync(path.join(actionDir, 'action.json'), `${JSON.stringify(out, null, 2)}\n`)
  console.log(`Generated action.json for ${actionDir} (TypeScript)`)
}
else if (fs.existsSync(goPath)) {
  // THE GO EXTRACTOR IS BUILT AND THEN RUN, RATHER THAN RUN THROUGH `go run`.
  //
  // `go run` is a launcher, and the status it exits with describes the
  // LAUNCHER: it collapses every non-zero status its program raises into 1 and
  // prints the real one as a line of text on stderr. A refusal reached this
  // process as an ordinary failure, so the branch below never fired for a Go
  // action and the stale action.json survived every refusal — leaving a tree
  // that reads as healthy to every later gate, because the file they read is
  // well-formed and describes a source that no longer exists.
  //
  // Recovering the status from that stderr line would be matching on a message
  // to decide whether to delete a file, which is the thing the distinct status
  // exists to replace. Building the extractor takes the launcher out of the
  // path instead, so the status the extractor raises is the status its caller
  // observes — and it stays that way for every caller, not only for one that
  // knows about a side channel.
  const goScriptPath = path.join(__dirname, 'extract_godoc.go')
  const buildDir = fs.mkdtempSync(path.join(os.tmpdir(), 'simple-extract-godoc-'))
  const extractorPath = path.join(buildDir, 'extract_godoc')
  const discardExtractor = () => fs.rmSync(buildDir, { force: true, recursive: true })

  // Only the extractor's own status is read as a refusal. `go` exits 2 for a
  // usage error of its own, and a build that failed has said nothing about
  // whether the action's exposure statement is well-formed.
  try {
    execFileSync('go', ['build', '-o', extractorPath, goScriptPath])
  }
  catch (err) {
    discardExtractor()
    console.error(`Failed to build the Go metadata extractor for ${actionDir}:`, err.message)
    if (err.stderr)
      console.error(err.stderr.toString())
    process.exit(1)
  }

  let outData
  try {
    outData = JSON.parse(execFileSync(extractorPath, ['--', goPath]).toString())
  }
  catch (err) {
    discardExtractor()

    // A Go action that cannot be described must fail the run, not leave the
    // stale action.json in place while the process exits cleanly. A silent
    // success here lets a metadata gate pass having verified nothing.
    //
    // A refusal reads the same to this generator's caller whichever language
    // the action is written in, and only a refusal discards the stale file. Its
    // own refusal has already reached this process's stderr, so it is not
    // restated under a heading that would make an author's mistake read as a
    // broken toolchain.
    if (err.status === ANNOTATION_REFUSAL_EXIT_CODE) {
      refuse(actionDir)
    }

    console.error(`Failed to extract GoDoc for ${actionDir}:`, err.message)
    if (err.stdout)
      console.error(err.stdout.toString())
    if (err.stderr)
      console.error(err.stderr.toString())
    process.exit(1)
  }

  discardExtractor()
  fs.writeFileSync(path.join(actionDir, 'action.json'), `${JSON.stringify(outData, null, 2)}\n`)
  console.log(`Generated action.json for ${actionDir} (Go)`)
}
else if (rustPath) {
  const rustData = rustCompanionOutput(actionDir, rustPath)

  // EVERY comment in the file is read for the exposure statement, not only the
  // one that supplied the description — the same rule the two branches above
  // hold to, and for the same reason. Which comment describes the action is
  // decided by where the payload is declared; where an author writes `@tool`
  // must not be decided by that as a side effect, because a dropped `@tool` is
  // an action that quietly stops being callable.
  //
  // The description is one of those comments, so its tags are counted once,
  // from the comments. It arrives RAW — the companion lifts nothing out of it,
  // because the names to lift are claimed here — and it is read a second time
  // below purely to take those lines out of the prose. Once, here, and nowhere
  // else: a companion that also lifted them would leave the rule with two homes
  // and the day the two disagreed, the description would either keep a `@tool` a
  // model reads as prose or lose a sentence its author wrote.
  const comments = rustData.comments.map(comment => splitRustDoc(comment))

  let ai
  try {
    ai = buildAiMetadata(
      path.basename(actionDir),
      comments.flatMap(comment => comment.tags),
      comments.flatMap(comment => comment.misspellings),
    )
  }
  catch (err) {
    console.error(err.message)
    refuse(actionDir)
  }

  const schema = rustData.schema
    && typeof rustData.schema === 'object'
    && !Array.isArray(rustData.schema)
    ? rustData.schema
    : noInputSchema()

  stripRustAnnotations(schema)

  const out = {
    description: splitRustDoc(rustData.description ?? '').description,
    schema,
  }

  if (ai) {
    out.ai = ai
  }

  fs.writeFileSync(path.join(actionDir, 'action.json'), `${JSON.stringify(out, null, 2)}\n`)
  console.log(`Generated action.json for ${actionDir} (Rust)`)
}
else {
  fs.writeFileSync(
    path.join(actionDir, 'action.json'),
    `${JSON.stringify({ description: '', schema: noInputSchema() }, null, 2)}\n`,
  )
  console.log(`Generated empty action.json for ${actionDir} (No supported source found)`)
}
