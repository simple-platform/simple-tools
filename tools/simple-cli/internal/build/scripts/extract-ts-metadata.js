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
const EFFECTS_TAG = 'effects'
const RETRY_TAG = 'retry'
const DISCLOSES_TAG = 'discloses'

const EXPOSURE_TAGS = [TOOL_TAG, EFFECTS_TAG, RETRY_TAG, DISCLOSES_TAG]

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

// The tags that say what CALLING a tool does. Each one qualifies `@tool`, so any
// of them written without it is a statement about nothing.
const QUALIFYING_TAGS = [EFFECTS_TAG, RETRY_TAG, DISCLOSES_TAG]

const EFFECT_VALUES = ['read', 'orchestration', 'write', 'destructive', 'external', 'credential']
const RETRY_VALUES = ['safe', 'keyed', 'verify-first', 'never']
const DISCLOSES_VALUES = ['tenant_record', 'settings_field', 'credential_field', 'secret_field']
const DEFAULT_DISCLOSES = 'tenant_record'

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
// An action that writes no exposure tag gets no `ai` object, which is how every
// action that is not a tool regenerates unchanged. Anything short of a complete,
// well-formed statement refuses instead of degrading, because a half-read
// annotation is how an action ends up advertised as something it is not.
function buildAiMetadata(action, tags, misspellings) {
  // A MISTYPED NAME IS REFUSED BEFORE ANYTHING ELSE IS READ.
  //
  // It is checked ahead of the tags because it explains them: an action missing
  // the tag it looks like it declares is missing it BECAUSE of this line, and a
  // refusal naming the incomplete statement would send its author to add a tag
  // they have already written.
  //
  // Refused even where the action declares nothing else, which is the case that
  // shipped. A lone mistyped `@discloses` left the action carrying the loosest
  // class by default and the line itself in the description, and nothing
  // anywhere said so.
  // The first one written, so fixing it and running again surfaces the next
  // rather than a list an author has to work through in one pass.
  const [near] = misspellings

  if (near) {
    throw annotationError(
      action,
      `writes @${near.written}, which nothing claims and which is one edit from @${near.meant}`,
      EXPOSURE_TAGS.map(tag => `@${tag}`),
    )
  }

  if (tags.length === 0) {
    return undefined
  }

  const declared = new Map()

  for (const tag of tags) {
    if (declared.has(tag.name)) {
      throw annotationError(action, `@${tag.name} is declared more than once`)
    }

    declared.set(tag.name, tag.value)
  }

  if (!declared.has(TOOL_TAG)) {
    // Named in vocabulary order rather than in the order they were declared, so
    // the same source is refused with the same sentence every time.
    const written = QUALIFYING_TAGS.filter(tag => declared.has(tag)).map(tag => `@${tag}`)

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

  if (!declared.has(EFFECTS_TAG)) {
    throw annotationError(action, `is a tool and must declare @${EFFECTS_TAG}`, EFFECT_VALUES)
  }

  if (!declared.has(RETRY_TAG)) {
    throw annotationError(action, `is a tool and must declare @${RETRY_TAG}`, RETRY_VALUES)
  }

  const retry = declared.get(RETRY_TAG)

  if (!RETRY_VALUES.includes(retry)) {
    throw annotationError(action, `@${RETRY_TAG} takes "${retry}"`, RETRY_VALUES)
  }

  const discloses = declared.has(DISCLOSES_TAG)
    ? declared.get(DISCLOSES_TAG)
    : DEFAULT_DISCLOSES

  if (!DISCLOSES_VALUES.includes(discloses)) {
    throw annotationError(action, `@${DISCLOSES_TAG} takes "${discloses}"`, DISCLOSES_VALUES)
  }

  // The member order below is the file format rather than a style choice: every
  // generated action.json states these four in this order, and the other
  // generator is held to the same order.
  /* eslint-disable perfectionist/sort-objects */
  return {
    tool: true,
    effects: parseEffects(action, declared.get(EFFECTS_TAG)),
    retry,
    discloses,
  }
  /* eslint-enable perfectionist/sort-objects */
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

function parseEffects(action, raw) {
  const effects = raw.split(/[\s,]+/).filter(Boolean)

  if (effects.length === 0) {
    throw annotationError(action, `@${EFFECTS_TAG} names no effect`, EFFECT_VALUES)
  }

  const seen = new Set()

  for (const effect of effects) {
    if (!EFFECT_VALUES.includes(effect)) {
      throw annotationError(action, `@${EFFECTS_TAG} names an unknown effect "${effect}"`, EFFECT_VALUES)
    }

    if (seen.has(effect)) {
      throw annotationError(action, `@${EFFECTS_TAG} names "${effect}" twice`, EFFECT_VALUES)
    }

    seen.add(effect)
  }

  return effects
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

    if (EXPOSURE_TAGS.includes(name)) {
      tags.push({ name, value: trimmed.slice(name.length + 1).trim() })
      continue
    }

    if (SCHEMA_TAGS.has(name)) {
      continue
    }

    // A near miss is left in the description rather than lifted out of it,
    // because it is refused before any description ships.
    const meant = name && EXPOSURE_TAGS.find(claimed => withinOneEdit(name, claimed))

    if (meant) {
      misspellings.push({ meant, written: name })
    }

    descriptionLines.push(line)
  }

  return { description: descriptionLines.join('\n').trim(), misspellings, tags }
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
else {
  fs.writeFileSync(
    path.join(actionDir, 'action.json'),
    `${JSON.stringify({ description: '', schema: noInputSchema() }, null, 2)}\n`,
  )
  console.log(`Generated empty action.json for ${actionDir} (No supported source found)`)
}
