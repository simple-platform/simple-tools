/* eslint-disable node/prefer-global/process */
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { BasicAnnotationsReader, createGenerator } from 'ts-json-schema-generator'
import { Project, SyntaxKind, ts } from 'ts-morph'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// THE AUTHOR-FACING VOCABULARY, AND IT IS ONE VOCABULARY FOR EVERY LANGUAGE.
//
// An action becomes callable by an agent because its own source says so, one
// tag per line, anywhere in a comment in the action's main file. Carrying it in
// the source is what lets regeneration keep it: this file rewrites action.json
// wholesale, so anything added to that file by hand is deleted the next time an
// author touches the action.
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
// `@shortdesc` and `@usewhen` are written for the MODEL rather than for the
// host: the first is the one line a tool listing shows and is REQUIRED wherever
// `@tool` is, and the second is repeatable and says when to reach for the tool.
// The doc comment's own prose is neither, and it is not touched: it is the full
// contract, and it arrives when the tool is selected rather than in the listing.
//
// `@parallelsafe` is the one name written for the HOST. It is a modifier tag
// like `@tool`, it is valid only where `@tool` is, and it says one thing: this
// tool only reads — it changes no stored data and sends nothing outward — so
// the host may run it at the same time as the other parallel-safe calls of one
// batch. It governs concurrency and nothing else. It never makes a call
// retryable and it never states what a call did to stored data; the host still
// treats every call as one whose effect it does not know. Nothing verifies the
// claim: the author owns it, and a tool that writes while claiming it is the
// author's defect, not something this generator can see.
//
// It is refused rather than dropped when `@tool` is absent, unlike the two
// listing tags. A dispatch statement on an action that is not a tool is a
// statement nobody acts on, and an author who wrote both and lost `@tool` is
// otherwise told nothing.
//
// `@Payload` is claimed too, though it states nothing about exposure: it names
// the type the schema is read from, so an author may point at a type not called
// `Payload`. It is claimed for the same reason the other four are — this
// generator READS it, and a directive to this generator that stayed in the
// prose would be shipped to a model as a sentence about what the action does.
// Claiming it also puts `@Payloud` in front of the misspelling rule below, where
// the alternative is a silent fall back to a type of the other name and a schema
// describing the wrong one.
//
// WHAT CALLING A TOOL DOES IS NOT IN THIS VOCABULARY, AND IT IS NOT ANYWHERE
// ELSE EITHER. `@effects`, `@retry` and `@discloses` are deleted outright. They
// were not relocated: a host-side table holding effects, retry safety and
// disclosure origin was designed, built, and deleted the same day, so nothing
// downstream states these about a tool and no action in any language has a way
// to declare them. `@parallelsafe` does not reopen that: it lets reads overlap,
// and it changes neither what a failed call is taken to have done to stored
// data nor whether a call may be tried again. This list does not name the
// three, and no list of retired names sits beside it either. An action that
// writes one is writing prose, and the line stays exactly where its author put
// it — the same answer this generator gives `@param` or any other name it does
// not claim.
//
// That is a smaller net than the one the misspelling rule casts, and the two are
// worth telling apart, because the retired spellings do not land the same way.
// `@short_desc` is one edit from `@shortdesc`, so it is REFUSED — as a
// misspelling of a claimed name, which is what an author who has not migrated
// has in fact written. `@when_use` is seven edits from `@usewhen`, so it is not
// refused at all and stays in the description like any other sentence. Neither
// outcome is a statement about the retired vocabulary: they fall out of how far
// each spelling happens to sit from a name this list does claim.
//
// A NAME ONE EDIT AWAY FROM A CLAIMED ONE IS REFUSED RATHER THAN LEFT AS PROSE.
// tsdoc.json declares these names to the editor and to ESLint, and that was
// taken here as the answer to a misspelling. It is not one: it reports `@toool`
// in an editor an author may not be running, it says nothing at build time, and
// the other generator reads Go doc comments that tsdoc.json does not reach at
// all — so the same typo stopped nothing in either language. What it costs is
// silent twice over: an action that meant to say `@shortdes` is offered to a
// model as a name and nothing else, and the line itself travels into the
// description that model reads.
//
// The host, not the author, pins a tool's revision: it is not in this
// vocabulary and there is nothing here for an author to get wrong about it.
const TOOL_TAG = 'tool'
const SHORTDESC_TAG = 'shortdesc'
const USEWHEN_TAG = 'usewhen'
const PARALLELSAFE_TAG = 'parallelsafe'
const PAYLOAD_TAG = 'Payload'

const ACTION_TAGS = [TOOL_TAG, SHORTDESC_TAG, USEWHEN_TAG, PARALLELSAFE_TAG, PAYLOAD_TAG]

// The type a schema is read from when the source names none.
const DEFAULT_PAYLOAD_TYPE = 'Payload'

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

// A LISTING HAS TO STAY SMALL, SO WHAT DOES NOT FIT IS REFUSED, NEVER DROPPED.
//
// Every `@shortdesc` and `@usewhen` an action writes is carried into the
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

// The exposure statement an action makes about itself, or nothing at all.
//
// ONE FUNCTION FOR EVERY LANGUAGE, because there is now one vocabulary. Two
// existed while a TypeScript or Go action declared what calling it did and a
// Rust action did not, and the two were told apart by nothing an author could
// see: the same doc comment was refused in one language and advertised in
// another, both at exit 0. With the three host-facing tags gone, what is left is
// the same five names, the same widths and the same refusals in the same order,
// so keeping two copies of them would only be keeping somewhere for the
// languages to drift apart.
//
// What genuinely differs between the languages is not in here. It is in the
// readers — which tags a description has already lost to a schema generator, and
// which type a payload is declared as — and each reader still states its own.
//
// THE VOCABULARY IS STATED HERE AND NOWHERE ELSE for the two languages this file
// reads itself: this generator writes action.json, and a rule about what may
// appear in that file that lives in a second program is a rule two programs get
// to disagree about. The Rust companion parses Rust — comments, types, serde
// attributes — and states no opinion about which tags are claimed, what they
// mean, or when a source is refused. The Go extractor is the one second copy
// that remains, because it is a second program in a second language, and the
// suite that drives both generators end to end is what holds them to one answer.
//
// An action that writes no tag gets no `ai` object, which is how every action
// that is not a tool regenerates unchanged. Anything short of a complete,
// well-formed statement refuses instead of degrading, because a half-read
// annotation is how an action ends up advertised as something it is not.
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

  // `@Payload` says which type the schema was read from, which the caller has
  // already acted on. It is not part of the statement about exposure, and an
  // action that writes it and nothing else is not making one.
  const stated = tags.filter(tag => tag.name !== PAYLOAD_TAG)

  if (stated.length === 0) {
    return undefined
  }

  const whenUse = stated.filter(tag => tag.name === USEWHEN_TAG).map(tag => tag.value)
  const declared = new Map()

  // `@usewhen` is the one repeatable name, so it is collected above rather
  // than refused here. Everything else may be written once: a second
  // `@shortdesc` is two answers to one question, and picking either is
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

  // `@parallelsafe` IS THE ONE STATEMENT REFUSED WITHOUT `@tool`.
  //
  // It says how the host may dispatch a tool, so on an action that is not one
  // it says something nobody will act on. Dropping it the way the listing tags
  // are dropped would also hide the likeliest cause: an author who wrote both
  // tags and lost `@tool` has an action that quietly stopped being callable.
  if (declared.has(PARALLELSAFE_TAG) && !declared.has(TOOL_TAG)) {
    throw annotationError(
      action,
      `@${PARALLELSAFE_TAG} says how a tool may be dispatched, and this action is not a tool. `
      + `Write @${TOOL_TAG} to expose it, or delete @${PARALLELSAFE_TAG}`,
    )
  }

  // NOTHING ELSE IS ASKED OF AN ACTION THAT IS NOT A TOOL.
  //
  // `@shortdesc` and `@usewhen` describe a tool to a model choosing between
  // tools. An action that never enters that listing has no use for either, so
  // writing one without `@tool` is not an error to refuse — it is a statement
  // about nothing, and the action simply gets no exposure block.
  //
  // The lines themselves do not reach the description. They are claimed names,
  // so they were already lifted out of the prose before this ran, and they are
  // dropped here rather than written anywhere. An author who wrote them and
  // omitted `@tool` gets a clean description and an action that is not a tool —
  // which is what they said, if not what they meant.
  //
  // That is the trade, and it is deliberate. Refusing here used to catch a
  // dropped `@tool` as a side effect; nothing catches it now unless the action
  // also wrote `@parallelsafe`. The near-miss rule still refuses `@toool`, but
  // no rule can refuse an absence.
  if (!declared.has(TOOL_TAG)) {
    return undefined
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

  // The same rule for the other modifier tag. A value here is most likely an
  // author qualifying the claim — `@parallelsafe reads only` — and the claim
  // has no qualified form: a tool either may run beside the others or may not.
  const parallelSafe = declared.has(PARALLELSAFE_TAG)
  const parallelValue = declared.get(PARALLELSAFE_TAG)

  if (parallelSafe && parallelValue !== '') {
    throw annotationError(
      action,
      `@${PARALLELSAFE_TAG} is a modifier tag and takes no value, and this one carries "${parallelValue}". `
      + 'Leave it bare to let the tool run beside other parallel-safe calls, or delete it to run it alone',
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

  // Present only when written, and never `false`: an unmarked tool is simply
  // not parallel-safe, the same way an unmarked action is not a tool.
  if (parallelSafe) {
    ai.parallelsafe = true
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
// Only the five names are claimed, so reading more comments cannot make an
// author's prose mean something: a line either is one of the five or is left
// exactly where it was written.
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

// The type a TypeScript action's schema is read from: the one its source names,
// or the conventional one when it names none.
//
// A name that is not written out is not a name this file may guess at, so an
// action pointing at a type it does not declare is described by the no-input
// schema — the same answer an action that declares no payload at all gets.
function payloadTypeNamed(tags) {
  const named = tags.find(tag => tag.name === PAYLOAD_TAG && tag.value !== '')

  return named ? named.value : DEFAULT_PAYLOAD_TYPE
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
// Two vocabularies are claimed and only one is returned. The five names above
// are this file's own and are what a caller asks for; the schema generator's
// tags are removed because it has already turned them into CONSTRAINTS, and a
// constraint stated twice — once as a keyword and once as English — is a member
// documented by whichever one the reader believes. `@description` is the
// exception and is kept, because it is the one of those that arrives as prose
// this file then replaces rather than as a constraint that survives.
//
// Everything else is what the author wrote: a `@param` or a `@remarks` stays
// where they put it. A name one edit from a claimed one is recorded rather than
// lifted, and its line stays in the description, because it is refused before
// any description ships.
function splitDoc(text) {
  return splitTags(text, SCHEMA_TAGS)
}

// The same line-wise reading, for a RUST action's doc comments.
//
// One difference from the reading above, and it is the whole reason a second
// entry point exists: THE SCHEMA GENERATOR'S TAGS ARE NOT REMOVED. `@minimum`
// and `@pattern` leave a TypeScript doc comment because a generator has already
// turned them into constraints beside the description, and a constraint stated
// twice is a member documented by whichever copy the reader believes. Nothing
// does that for Rust — there is no constraint vocabulary for a Rust member and
// none has been ruled on — so removing those lines here would delete the
// author's sentence and put nothing in its place. The gaps the companion reports
// are how an author hears about that instead.
//
// The claimed names and the misspelling rule are the SAME ones, because there is
// one vocabulary. So `@effects` on a Rust action and `@effects` on a TypeScript
// action land identically: nothing claims the name and nothing is one edit from
// it, so the line stays exactly where its author wrote it, while `@short_desc` —
// one edit from `@shortdesc` — is refused in both.
function splitRustDoc(text) {
  return splitTags(text, new Set())
}

// The reading both of the above are: the claimed names lifted out line by line,
// the named set discarded, and everything else kept exactly where it was
// written.
function splitTags(text, discarded) {
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

    if (discarded.has(name)) {
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

  // EVERY comment in the file is read for the annotations, not only the one that
  // supplies the description.
  //
  // Which comment describes the action is decided below, by rules about where a
  // payload is declared. Where an author writes the statement must not be
  // decided by those rules as a side effect: a tag written in a comment that did
  // not win would be dropped in silence, and a dropped `@tool` is an action that
  // quietly stops being callable — the failure this whole annotation exists to
  // make impossible.
  const comments = commentsIn(sourceFile.getFullText()).map(splitDoc)

  const statedTags = comments.flatMap(comment => comment.tags)
  const misspellings = comments.flatMap(comment => comment.misspellings)

  // The two blocks an author may describe an action in: the payload interface,
  // and the handler when the interface says nothing.
  const payloadType = payloadTypeNamed(statedTags)
  const payloadInterface = sourceFile.getInterface(payloadType)

  const handlerFunc = sourceFile.getFunction('handler') || sourceFile.getVariableDeclaration('handler')
  const handlerNode = handlerFunc && handlerFunc.getKindName() === 'VariableDeclaration'
    ? handlerFunc.getFirstAncestorByKind(SyntaxKind.VariableStatement)
    : handlerFunc

  // WHERE THE PROSE IS WHEN `@Payload` POINTS AT A TYPE THAT DOES NOT CARRY IT.
  //
  // The two blocks above are the ones a reader looks at, and they are tried in
  // that order. An author who names a payload type other than the block they
  // wrote the statement in has put their prose in a third place: the block
  // carrying the tags. Reading only the two would drop it, and the action would
  // advertise an input shape while stating nothing about what it does — refused
  // downstream, correctly, but named as a missing description rather than as a
  // sentence this reading failed to find, which sends its author looking for the
  // wrong thing.
  const stating = comments.find(comment => comment.tags.length > 0)

  const description = describedBy(payloadInterface)
    || describedBy(handlerNode)
    || (stating ? stating.description : '')

  let ai
  try {
    ai = buildAiMetadata(path.basename(actionDir), statedTags, misspellings)
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
        type: payloadType,
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
