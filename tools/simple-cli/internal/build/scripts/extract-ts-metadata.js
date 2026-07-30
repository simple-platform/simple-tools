/* eslint-disable node/prefer-global/process */
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createGenerator } from 'ts-json-schema-generator'
import { Project, SyntaxKind } from 'ts-morph'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
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

  let description = ''

  // Try to find the Payload interface to get its description
  const payloadInterface = sourceFile.getInterface('Payload')
  if (payloadInterface) {
    const jsDocs = payloadInterface.getJsDocs()
    if (jsDocs.length > 0) {
      description = jsDocs[0].getDescription().trim()
    }
  }

  // If no description on Payload, fallback to handler function's description
  if (!description) {
    const handlerFunc = sourceFile.getFunction('handler') || sourceFile.getVariableDeclaration('handler')
    if (handlerFunc) {
      const nodeWithDocs = handlerFunc.getKindName() === 'VariableDeclaration'
        ? handlerFunc.getFirstAncestorByKind(SyntaxKind.VariableStatement)
        : handlerFunc

      if (nodeWithDocs) {
        const jsDocs = nodeWithDocs.getJsDocs()
        if (jsDocs.length > 0) {
          description = jsDocs[0].getDescription().trim()
        }
      }
    }
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
