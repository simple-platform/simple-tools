/* eslint-disable node/prefer-global/process */
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createGenerator } from 'ts-json-schema-generator'
import { Project, SyntaxKind } from 'ts-morph'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// THE AUTHOR-FACING EXPOSURE VOCABULARY.
//
// An action becomes callable by an agent because its own doc comment says so,
// one tag per line, in the same block the description is read from. Carrying it
// in the source is what lets regeneration keep it: this file rewrites
// action.json wholesale, so anything added to that file by hand is deleted the
// next time an author touches the action.
//
// Exposure is opt-in and there is no blocklist. An action that declares nothing
// is not a tool, so a new action is unreachable by an agent until its author
// writes the sentence that reaches it — rather than reachable until someone
// remembers to exclude it.
//
// The host, not the author, pins a tool's revision: it is not in this
// vocabulary and there is nothing here for an author to get wrong about it.
const AI_TAG_PREFIX = 'ai_'
const AI_TOOL_TAG = 'ai_tool'
const AI_EFFECTS_TAG = 'ai_effects'
const AI_RETRY_SAFETY_TAG = 'ai_retry_safety'
const AI_DISCLOSURE_ORIGIN_TAG = 'ai_disclosure_origin'

const AI_TAGS = [AI_TOOL_TAG, AI_EFFECTS_TAG, AI_RETRY_SAFETY_TAG, AI_DISCLOSURE_ORIGIN_TAG]
const AI_TOOL_VALUES = ['true', 'false']
const AI_EFFECTS = ['read', 'orchestration', 'write', 'destructive', 'external', 'credential']
const AI_RETRY_SAFETIES = ['safe', 'idempotent_with_key', 'verify_before_retry', 'never_automatic']
const AI_DISCLOSURE_ORIGINS = ['tenant_record', 'settings_field', 'credential_field', 'secret_field']
const AI_DEFAULT_DISCLOSURE_ORIGIN = 'tenant_record'

function annotationError(action, message, accepted) {
  const suffix = accepted ? ` Accepted: ${accepted.join(', ')}.` : ''
  return new Error(`${action}: ${message}.${suffix}`)
}

function applyJsDocConstraints(schema, payloadInterface) {
  if (!schema || typeof schema !== 'object' || !payloadInterface) {
    return
  }

  const properties = schema.properties
  if (!properties || typeof properties !== 'object') {
    return
  }

  for (const property of payloadInterface.getProperties()) {
    const propertyName = property.getName()
    const propertySchema = properties[propertyName]

    if (!propertySchema || typeof propertySchema !== 'object') {
      continue
    }

    for (const jsDoc of property.getJsDocs()) {
      for (const tag of jsDoc.getTags()) {
        if (tag.getTagName() === 'maxLength') {
          const rawValue = tag.getCommentText() || tag.getComment() || ''
          const maxLength = Number.parseInt(String(rawValue).trim(), 10)

          if (Number.isInteger(maxLength) && maxLength > 0) {
            propertySchema.maxLength = maxLength
          }
        }
      }
    }
  }
}

// The exposure statement an action makes about itself, or nothing at all.
//
// Absent means false: an action that writes no `@ai_` tag gets no `ai` object,
// which is how every action that is not a tool regenerates unchanged. Anything
// short of a complete, well-formed statement refuses instead of degrading,
// because a half-read annotation is how an action ends up advertised as
// something it is not.
function buildAiMetadata(action, tags) {
  if (tags.length === 0) {
    return undefined
  }

  const declared = new Map()

  for (const tag of tags) {
    if (!AI_TAGS.includes(tag.name)) {
      throw annotationError(action, `@${tag.name} is not an exposure annotation`, AI_TAGS.map(name => `@${name}`))
    }

    if (declared.has(tag.name)) {
      throw annotationError(action, `@${tag.name} is declared more than once`)
    }

    declared.set(tag.name, tag.value)
  }

  if (!declared.has(AI_TOOL_TAG)) {
    throw annotationError(
      action,
      `declares an exposure annotation without @${AI_TOOL_TAG}, so it is not a tool and the rest says nothing`,
      AI_TOOL_VALUES,
    )
  }

  const tool = declared.get(AI_TOOL_TAG)

  if (!AI_TOOL_VALUES.includes(tool)) {
    throw annotationError(action, `@${AI_TOOL_TAG} takes "${tool}"`, AI_TOOL_VALUES)
  }

  if (tool === 'false') {
    for (const tag of [AI_EFFECTS_TAG, AI_RETRY_SAFETY_TAG, AI_DISCLOSURE_ORIGIN_TAG]) {
      if (declared.has(tag)) {
        throw annotationError(action, `@${tag} qualifies a tool, and @${AI_TOOL_TAG} is false`)
      }
    }

    return { tool: false }
  }

  if (!declared.has(AI_EFFECTS_TAG)) {
    throw annotationError(action, `is a tool and must declare @${AI_EFFECTS_TAG}`, AI_EFFECTS)
  }

  if (!declared.has(AI_RETRY_SAFETY_TAG)) {
    throw annotationError(action, `is a tool and must declare @${AI_RETRY_SAFETY_TAG}`, AI_RETRY_SAFETIES)
  }

  const retrySafety = declared.get(AI_RETRY_SAFETY_TAG)

  if (!AI_RETRY_SAFETIES.includes(retrySafety)) {
    throw annotationError(action, `@${AI_RETRY_SAFETY_TAG} takes "${retrySafety}"`, AI_RETRY_SAFETIES)
  }

  const disclosureOrigin = declared.has(AI_DISCLOSURE_ORIGIN_TAG)
    ? declared.get(AI_DISCLOSURE_ORIGIN_TAG)
    : AI_DEFAULT_DISCLOSURE_ORIGIN

  if (!AI_DISCLOSURE_ORIGINS.includes(disclosureOrigin)) {
    throw annotationError(
      action,
      `@${AI_DISCLOSURE_ORIGIN_TAG} takes "${disclosureOrigin}"`,
      AI_DISCLOSURE_ORIGINS,
    )
  }

  // The member order below is the file format rather than a style choice: every
  // generated action.json states these four in this order, and the other
  // generator is held to the same order.
  /* eslint-disable perfectionist/sort-objects */
  return {
    tool: true,
    effects: parseEffects(action, declared.get(AI_EFFECTS_TAG)),
    retry_safety: retrySafety,
    disclosure_origin: disclosureOrigin,
  }
  /* eslint-enable perfectionist/sort-objects */
}

// Every `@ai_*` tag written in one doc block, in the order the author wrote it.
//
// The tags are read off the same block the description comes from, and the
// description is the text BEFORE them, so an annotation can never reach the
// model as part of what the tool says about itself.
function collectAiTags(jsDoc, into) {
  if (!jsDoc) {
    return
  }

  for (const tag of jsDoc.getTags()) {
    const name = tag.getTagName()

    if (!name.startsWith(AI_TAG_PREFIX)) {
      continue
    }

    const comment = tag.getCommentText() || tag.getComment() || ''
    into.push({ name, value: String(comment).split('\n')[0].trim() })
  }
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
    throw annotationError(action, `@${AI_EFFECTS_TAG} names no effect`, AI_EFFECTS)
  }

  const seen = new Set()

  for (const effect of effects) {
    if (!AI_EFFECTS.includes(effect)) {
      throw annotationError(action, `@${AI_EFFECTS_TAG} names an unknown effect "${effect}"`, AI_EFFECTS)
    }

    if (seen.has(effect)) {
      throw annotationError(action, `@${AI_EFFECTS_TAG} names "${effect}" twice`, AI_EFFECTS)
    }

    seen.add(effect)
  }

  return effects
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
  const payloadDoc = payloadInterface ? payloadInterface.getJsDocs()[0] : undefined

  const handlerFunc = sourceFile.getFunction('handler') || sourceFile.getVariableDeclaration('handler')
  const handlerNode = handlerFunc && handlerFunc.getKindName() === 'VariableDeclaration'
    ? handlerFunc.getFirstAncestorByKind(SyntaxKind.VariableStatement)
    : handlerFunc
  const handlerDoc = handlerNode ? handlerNode.getJsDocs()[0] : undefined

  let description = payloadDoc ? payloadDoc.getDescription().trim() : ''

  if (!description && handlerDoc) {
    description = handlerDoc.getDescription().trim()
  }

  // Both blocks are read for exposure annotations even though only one of them
  // supplies the description. A tag written in the block that did not win would
  // otherwise be dropped in silence, and a dropped `@ai_tool` is an action that
  // quietly stops being callable — the failure this whole annotation exists to
  // make impossible.
  const aiTags = []
  collectAiTags(payloadDoc, aiTags)
  collectAiTags(handlerDoc, aiTags)

  let ai
  try {
    ai = buildAiMetadata(path.basename(actionDir), aiTags)
  }
  catch (err) {
    console.error(err.message)
    process.exit(1)
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
      applyJsDocConstraints(schema, payloadInterface)
    }
    catch (err) {
      if (err.message && !err.message.includes('No root type')) {
        console.error(`Failed to generate schema for ${actionDir}:`, err)
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
  // Use Go script
  try {
    const goScriptPath = path.join(__dirname, 'extract_godoc.go')
    const outBuf = execFileSync('go', ['run', goScriptPath, '--', goPath])
    const outStr = outBuf.toString()
    const outData = JSON.parse(outStr)
    fs.writeFileSync(path.join(actionDir, 'action.json'), `${JSON.stringify(outData, null, 2)}\n`)
    console.log(`Generated action.json for ${actionDir} (Go)`)
  }
  catch (err) {
    console.error(`Failed to extract GoDoc for ${actionDir}:`, err.message)
    if (err.stdout)
      console.error(err.stdout.toString())
    if (err.stderr)
      console.error(err.stderr.toString())
  }
}
else {
  fs.writeFileSync(
    path.join(actionDir, 'action.json'),
    `${JSON.stringify({ description: '', schema: noInputSchema() }, null, 2)}\n`,
  )
  console.log(`Generated empty action.json for ${actionDir} (No supported source found)`)
}
