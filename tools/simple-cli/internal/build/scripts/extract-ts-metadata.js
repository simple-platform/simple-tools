/* eslint-disable node/prefer-global/process */
import fs from 'node:fs'
import path from 'node:path'
import { createGenerator } from 'ts-json-schema-generator'
import { Project, SyntaxKind } from 'ts-morph'

let actionDir = process.argv[2]
if (!actionDir) {
  console.error('Usage: node extract-ts-metadata.js <action_dir>')
  process.exit(1)
}

// If it's not absolute, resolve it relative to process.cwd()
if (!path.isAbsolute(actionDir)) {
  actionDir = path.resolve(process.cwd(), actionDir)
}

const tsPath = path.join(actionDir, 'src', 'index.ts')

if (!fs.existsSync(tsPath)) {
  console.error(`TypeScript source not found: ${tsPath}`)
  process.exit(1)
}

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
let schema = {}
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
  }
  catch (err) {
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

// Write with trailing newline (POSIX compliance)
fs.writeFileSync(path.join(actionDir, 'action.json'), `${JSON.stringify(out, null, 2)}\n`)
console.log(`Generated action.json for ${actionDir}`)
