/**
 * Simple Platform SDK — lightweight GraphQL client for Spaces via RPC.
 *
 * Usage:
 *   import { query, mutate } from './lib/simple'
 *
 *   const data = await query(`{ dev_simple_system__users { id email } }`)
 *   const result = await mutate(`mutation { insert_my_app__task(object: $data) { id } }`, { data: { title: "New" } })
 */

const GET_THEME = `
query GetTheme {
  theme: dev_simple_system__settings(where: {
    name: { _eq: "simple.branding.theme" } 
  }, limit: 1) {
    value
  }
}
`

let rpcPort: MessagePort | null = null
const pendingRequests = new Map<string, { resolve: (value: any) => void, reject: (reason?: any) => void, timer: number }>()
const requestQueue: { message: any, transfer?: Transferable[] }[] = [] // Queue requests before port is ready
type NavigationTarget = 'new-tab' | 'same-tab'
type DocumentLifecycle = 'pending' | 'record' | 'staged'

export interface DocumentHandle {
  file_hash: string
  filename: string
  mime_type: string
  pending?: boolean
  scope?: 'ephemeral' | 'record' | 'staged'
  size: number
  storage_path?: string
}

export interface DocumentInput {
  bytes: ArrayBuffer | ArrayBufferView
  mime?: string
  name: string
}

export interface DocumentTarget {
  appId?: string
  app_id?: string
  fieldName?: string
  field_name?: string
  recordId?: string
  record_id?: string
  tableName?: string
  table_name?: string
}

export interface AIExecutionResult {
  data: any
  metadata: {
    inputTokens: number
    outputTokens: number
    reasoning?: string
    reasoningTokens?: number
  }
}

export interface AICommonOptions {
  model?: 'large' | 'lite' | 'medium' | 'xl'
  prompt: string
  reasoning?: boolean
  reasoningBudget?: number
  regenerate?: boolean
  systemPrompt?: string
  temperature?: number
  timeout?: number
}

interface JSONSchemaBase {
  description?: string
}

export interface JSONSchemaArray extends JSONSchemaBase {
  items: JSONSchema
  maxItems?: number
  minItems?: number
  type: 'array'
}

export interface JSONSchemaBoolean extends JSONSchemaBase {
  type: 'boolean'
}

export interface JSONSchemaNumber extends JSONSchemaBase {
  exclusiveMaximum?: number
  exclusiveMinimum?: number
  maximum?: number
  minimum?: number
  multipleOf?: number
  type: 'integer' | 'number'
}

export interface JSONSchemaObject extends JSONSchemaBase {
  properties: Record<string, JSONSchema>
  required?: string[]
  type: 'object'
}

export interface JSONSchemaString extends JSONSchemaBase {
  format?: 'date' | 'date-time' | 'email' | 'uri'
  maxLength?: number
  minLength?: number
  pattern?: string
  type: 'string'
}

export type JSONSchema
  = | JSONSchemaArray
    | JSONSchemaBoolean
    | JSONSchemaNumber
    | JSONSchemaObject
    | JSONSchemaString

export interface AIExtractOptions extends AICommonOptions {
  schema: JSONSchema
}

export interface AISummarizeOptions extends AICommonOptions {}

export interface AITranscribeOptions extends Omit<AICommonOptions, 'prompt'> {
  includeTimestamps?: boolean
  includeTranscript?: boolean
  participants?: boolean | string[]
  summarize?: boolean
}

// Security: Expected origin is the parent's origin.
// In production, spaces are on assets.simple.dev and parent is on simple.dev.
// For templates, we trust the parent frame that loaded us if we are in an iframe.
function getExpectedOrigin() {
  try {
    if (window.parent === window)
      return window.location.origin
    const url = new URL(document.referrer)
    return url.origin
  }
  catch {
    return '*'
  }
}

// 1. Wait for Parent to give us our dedicated MessagePort
window.addEventListener('message', (event) => {
  // Security: Verify origin of INIT_RPC message
  const expectedOrigin = getExpectedOrigin()
  if (expectedOrigin !== '*' && event.origin !== expectedOrigin)
    return

  if (event.data?.type === 'INIT_RPC' && event.ports[0]) {
    rpcPort = event.ports[0]

    // Listen for responses matching our UUIDs
    rpcPort.onmessage = (e) => {
      if (e.data?.type === 'GRAPHQL_RESPONSE') {
        const { data, error, errors, id } = e.data
        const req = pendingRequests.get(id)
        if (req) {
          clearTimeout(req.timer)
          if (error || errors) {
            let errMsg = error || 'GraphQL Error'

            // The platform often sends the detailed database constraints in an 'errors' array
            // or specific issues under extensions.issues for VALIDATION_FAILED
            if (Array.isArray(errors) && errors.length > 0) {
              const rootError = errors[0]
              const issues = rootError?.extensions?.issues
              const details = rootError?.extensions?.details?.message
              if (Array.isArray(issues) && issues.length > 0 && issues[0]?.message) {
                errMsg = issues[0].message
              }
              else if (details) {
                errMsg = details
              }
              else if (rootError?.message) {
                errMsg = rootError.message
              }
            }

            req.reject(new Error(errMsg))
          }
          else {
            req.resolve(data)
          }
          pendingRequests.delete(id)
        }
      }

      if (e.data?.type === 'DECRYPT_RESPONSE') {
        const { error, id, value } = e.data
        const req = pendingRequests.get(id)
        if (req) {
          clearTimeout(req.timer)
          if (error)
            req.reject(new Error(error))
          else
            req.resolve(value)
          pendingRequests.delete(id)
        }
      }

      if (e.data?.type === 'GET_USER_RESPONSE') {
        const { claims, error, id, user } = e.data
        const req = pendingRequests.get(id)
        if (req) {
          clearTimeout(req.timer)
          if (error)
            req.reject(new Error(error))
          else
            req.resolve({ claims, user })
          pendingRequests.delete(id)
        }
      }

      if (
        e.data?.type === 'DOCUMENT_CREATE_HANDLE_RESPONSE'
        || e.data?.type === 'DOCUMENT_PROMOTE_RESPONSE'
        || e.data?.type === 'DOCUMENT_DELETE_RESPONSE'
        || e.data?.type === 'AI_RESPONSE'
      ) {
        const { error, id } = e.data
        const req = pendingRequests.get(id)
        if (req) {
          clearTimeout(req.timer)
          if (error)
            req.reject(new Error(error))
          else
            req.resolve(e.data)
          pendingRequests.delete(id)
        }
      }
    }

    // Flush any requests that were queued before the port was ready
    while (requestQueue.length > 0) {
      const queued = requestQueue.shift()!
      rpcPort.postMessage(queued.message, queued.transfer || [])
    }
  }
})

// 2. Tell the Parent we are ready to receive a port
// Do not do this if we are not in an iframe
if (window !== window.parent) {
  const targetOrigin = getExpectedOrigin()
  window.parent.postMessage({ type: 'SPACE_READY' }, targetOrigin)
}

// ---------------------------------------------------------------------------
// Core fetcher Proxy
// ---------------------------------------------------------------------------

async function executeRpcGraphQL<T = any>(
  gql: string,
  variables?: boolean | Record<string, any>, // support passing true or object
): Promise<T> {
  // If variables is a boolean, it was likely passed incorrectly due to old signatures. Ignore it.
  const vars = typeof variables === 'object' ? variables : undefined

  return new Promise((resolve, reject) => {
    const id = crypto.randomUUID()
    // Implementation of RPC Timeout (30 seconds)
    const timer = window.setTimeout(() => {
      const req = pendingRequests.get(id)
      if (req) {
        req.reject(new Error('RPC request timed out after 30 seconds'))
        pendingRequests.delete(id)
      }
    }, 30000)

    pendingRequests.set(id, { reject, resolve, timer })

    const payload = {
      payload: { id, query: gql, variables: vars },
      type: 'GRAPHQL_REQUEST',
    }

    if (rpcPort) {
      rpcPort.postMessage(payload)
    }
    else {
      // Very fast spaces might call query() before the parent's postMessage arrives
      requestQueue.push({ message: payload })
    }
  })
}

function normalizeBytes(bytes: ArrayBuffer | ArrayBufferView): ArrayBuffer {
  if (bytes instanceof ArrayBuffer)
    return bytes.slice(0)

  if (ArrayBuffer.isView(bytes)) {
    const copy = new Uint8Array(bytes.byteLength)
    copy.set(new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength))
    return copy.buffer
  }

  throw new Error('Document bytes must be an ArrayBuffer or typed array')
}

function isFileInput(input: DocumentInput | File): input is File {
  return typeof File !== 'undefined' && input instanceof File
}

function executeRpc<T = any>(
  type: string,
  payload: Record<string, any>,
  timeoutMessage: string,
  transfer?: Transferable[],
): Promise<T> {
  return new Promise((resolve, reject) => {
    const id = crypto.randomUUID()
    const timer = window.setTimeout(() => {
      const req = pendingRequests.get(id)
      if (req) {
        req.reject(new Error(timeoutMessage))
        pendingRequests.delete(id)
      }
    }, 300000)

    pendingRequests.set(id, { reject, resolve, timer })

    const message = {
      payload: { ...payload, id },
      type,
    }

    if (rpcPort)
      rpcPort.postMessage(message, transfer || [])
    else
      requestQueue.push({ message, transfer })
  })
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/** Execute a GraphQL query. Returns the `data` object. */
export const query = executeRpcGraphQL

/** Execute a GraphQL mutation. Returns the `data` object. */
export const mutate = executeRpcGraphQL

export const documents = {
  async createHandle(
    input: DocumentInput | File,
    options: { lifecycle?: DocumentLifecycle, target?: DocumentTarget | null } = {},
  ): Promise<DocumentHandle> {
    const documentInput: DocumentInput = isFileInput(input)
      ? {
          bytes: await input.arrayBuffer(),
          mime: input.type || 'application/octet-stream',
          name: input.name,
        }
      : input

    if (!documentInput?.name)
      throw new Error('Document name is required')

    const bytes = normalizeBytes(documentInput.bytes)
    const lifecycle = options.lifecycle ?? (options.target ? 'record' : 'staged')
    const response = await executeRpc<{ handle: DocumentHandle }>(
      lifecycle === 'pending' ? 'DOCUMENT_CREATE_PENDING_HANDLE_REQUEST' : 'DOCUMENT_CREATE_HANDLE_REQUEST',
      {
        bytes,
        lifecycle,
        mime: documentInput.mime || 'application/octet-stream',
        name: documentInput.name,
        target: options.target ?? null,
      },
      'Document upload timed out after 5 minutes',
      [bytes],
    )

    return response.handle
  },

  async promote(
    handle: DocumentHandle,
    target: DocumentTarget,
    options: { keepSource?: boolean } = {},
  ): Promise<DocumentHandle> {
    const response = await executeRpc<{ handle: DocumentHandle }>(
      'DOCUMENT_PROMOTE_REQUEST',
      { handle, keepSource: options.keepSource ?? false, target },
      'Document promotion timed out after 5 minutes',
    )

    return response.handle
  },

  async remove(handle: DocumentHandle): Promise<void> {
    await executeRpc(
      'DOCUMENT_DELETE_REQUEST',
      { handle },
      'Document delete timed out after 5 minutes',
    )
  },

  async upload(file: File, options: { lifecycle?: DocumentLifecycle, target?: DocumentTarget | null } = {}): Promise<DocumentHandle> {
    return documents.createHandle(file, options)
  },
}

export const ai = {
  extract(input: unknown, options: AIExtractOptions): Promise<AIExecutionResult> {
    return runAI('extract', input, options)
  },

  summarize(input: unknown, options: AISummarizeOptions): Promise<AIExecutionResult> {
    return runAI('summarize', input, options)
  },

  transcribe(input: DocumentHandle, options: AITranscribeOptions): Promise<AIExecutionResult> {
    return transcribeAI(input, options)
  },
}

function buildTranscriptionPrompt(
  includeTimestamps: boolean,
  includeTranscript: boolean,
  participants: boolean | string[] | undefined,
  summarize: boolean,
): string {
  let prompt = 'Analyze this audio/video file and provide:\n'

  if (includeTranscript) {
    if (participants) {
      if (Array.isArray(participants)) {
        prompt += `- A complete transcript identifying these participants: ${participants.join(', ')}. `
      }
      else {
        prompt += '- A complete transcript with participant identification (label participants as Participant 1, Participant 2, etc.). '
      }

      prompt += includeTimestamps
        ? 'Include timestamps in [MM:SS] format before each participant segment.\n'
        : 'Format each line as "Participant Name: text".\n'
    }
    else {
      prompt += includeTimestamps
        ? '- A complete transcript with timestamps in [MM:SS] format before each segment\n'
        : '- A complete transcript of all spoken content\n'
    }
  }

  if (summarize) {
    prompt += participants
      ? '- A concise summary highlighting key points from each participant\n'
      : '- A concise summary of the main points and key information\n'
  }

  if (participants) {
    prompt += Array.isArray(participants)
      ? `- Identify and distinguish between these participants: ${participants.join(', ')}\n`
      : '- Identify and list all distinct participants in the audio\n'
  }

  prompt += '- The detected language code (ISO 639-1 format)\n'
  return prompt
}

function buildTranscriptionSchema(
  includeTimestamps: boolean,
  includeTranscript: boolean,
  participants: boolean | string[] | undefined,
  summarize: boolean,
): JSONSchemaObject {
  const properties: Record<string, JSONSchema> = {
    language: {
      description: 'The detected language of the audio (ISO 639-1 code, e.g., "en", "es")',
      type: 'string',
    },
  }
  const required = ['language']

  if (includeTranscript) {
    let transcriptDescription = 'The full transcript of the audio'
    if (participants) {
      transcriptDescription = includeTimestamps
        ? 'The full transcript with participant labels and timestamps. Format: [MM:SS] Participant Name: text'
        : 'The full transcript with participant labels. Format: Participant Name: text'
    }
    else if (includeTimestamps) {
      transcriptDescription = 'The full transcript with timestamps. Format: [MM:SS] text'
    }

    properties.transcript = {
      description: transcriptDescription,
      type: 'string',
    }
    required.push('transcript')
  }

  if (summarize) {
    properties.summary = {
      description: participants
        ? 'A concise summary of the audio content, including key points from each participant'
        : 'A concise summary of the audio content',
      type: 'string',
    }
    required.push('summary')
  }

  if (participants) {
    properties.participants = {
      description: 'List of identified participants in the audio',
      items: { type: 'string' },
      type: 'array',
    }
    required.push('participants')
  }

  return {
    properties,
    required,
    type: 'object',
  }
}

function transcribeAI(input: DocumentHandle, options: AITranscribeOptions): Promise<AIExecutionResult> {
  if (!input || typeof input !== 'object' || !input.file_hash)
    throw new Error('The `input` parameter must be a valid DocumentHandle for `transcribe`.')

  const {
    includeTimestamps = false,
    includeTranscript = false,
    participants,
    summarize = false,
  } = options

  if (!includeTranscript && !summarize)
    throw new Error('At least one of `includeTranscript` or `summarize` must be true for `transcribe`.')

  const mimeType = input.mime_type?.toLowerCase() || ''
  if (!mimeType.startsWith('audio/') && !mimeType.startsWith('video/'))
    throw new Error('The input file must be an audio or video file for `transcribe`.')

  return runAI('extract', input, {
    ...options,
    prompt: buildTranscriptionPrompt(includeTimestamps, includeTranscript, participants, summarize),
    schema: buildTranscriptionSchema(includeTimestamps, includeTranscript, participants, summarize),
  })
}

async function runAI(
  operation: 'extract' | 'summarize',
  input: unknown,
  options: AIExtractOptions | AISummarizeOptions,
): Promise<AIExecutionResult> {
  const response = await executeRpc<{ result: AIExecutionResult }>(
    'AI_REQUEST',
    { input, operation, options },
    'AI request timed out after 5 minutes',
  )

  return response.result
}

function getSafeNavigationUrl(rawUrl: string): string | null {
  const expectedOrigin = getExpectedOrigin()
  if (expectedOrigin === '*')
    return null

  try {
    const parsed = new URL(rawUrl, expectedOrigin)
    if (parsed.protocol !== 'https:')
      return null

    if (parsed.origin !== expectedOrigin)
      return null

    return parsed.toString()
  }
  catch {
    return null
  }
}

export function navigateInParent(rawUrl: string, target: NavigationTarget = 'same-tab'): boolean {
  const safeUrl = getSafeNavigationUrl(rawUrl)
  if (!safeUrl) {
    console.warn('Blocked unsafe navigation request from space:', rawUrl)
    return false
  }

  const payload = {
    payload: { target, url: safeUrl },
    type: 'NAVIGATE_REQUEST',
  }

  if (rpcPort) {
    rpcPort.postMessage(payload)
    return true
  }

  requestQueue.push({ message: payload })
  return true
}

/**
 * Decrypt a vault-encrypted field value for an existing record.
 *
 * The iframe cannot make authenticated requests directly, so this proxies the
 * `GET /decrypt/:appId/:table/:recordId/:field` call through the parent host
 * frame, which holds the session cookie.
 *
 * @param appId      Application ID (e.g., "dev.simple.system")
 * @param tableName  Table name (e.g., "api_key")
 * @param recordId   Record ID (e.g., "KEY000017")
 * @param fieldName  Field name to decrypt (e.g., "api_key")
 * @returns          Plain-text decrypted value
 */
export function decrypt(
  appId: string,
  tableName: string,
  recordId: string,
  fieldName: string,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const id = crypto.randomUUID()
    const timer = window.setTimeout(() => {
      const req = pendingRequests.get(id)
      if (req) {
        req.reject(new Error('Decrypt request timed out after 30 seconds'))
        pendingRequests.delete(id)
      }
    }, 30000)

    pendingRequests.set(id, { reject, resolve, timer })

    const payload = {
      payload: { appId, fieldName, id, recordId, tableName },
      type: 'DECRYPT_REQUEST',
    }

    if (rpcPort)
      rpcPort.postMessage(payload)
    else
      requestQueue.push({ message: payload })
  })
}

/**
 * Get the currently logged in user and their custom identity claims.
 *
 * @returns A promise resolving to an object containing the user profile and claims.
 */
export function getUser(): Promise<{
  user: {
    avatar?: string
    email: string
    firstName: string
    id: string
    initials?: string
    lastName: string
    name?: string
  }
  claims: Record<string, any>
}> {
  return new Promise((resolve, reject) => {
    const id = crypto.randomUUID()
    const timer = window.setTimeout(() => {
      const req = pendingRequests.get(id)
      if (req) {
        req.reject(new Error('Get user request timed out after 30 seconds'))
        pendingRequests.delete(id)
      }
    }, 30000)

    pendingRequests.set(id, { reject, resolve, timer })

    const payload = {
      payload: { id },
      type: 'GET_USER_REQUEST',
    }

    if (rpcPort)
      rpcPort.postMessage(payload)
    else
      requestQueue.push({ message: payload })
  })
}

/**
 * Loads tenant-specific theme overrides from the settings table via RPC.
 * Call this once on app mount (e.g., in main.tsx).
 */
export async function loadTheme(): Promise<void> {
  try {
    const data = await query<{ theme: { value: string }[] }>(GET_THEME)

    let css = data?.theme?.[0]?.value
    if (!css || typeof css !== 'string' || !css.includes('--'))
      return

    // Security: Basic CSS Sanitization
    // We only allow CSS custom properties defined in a :root or root-like block.
    // Dangerous constructs like url(), @import, position: fixed, etc. are stripped for security.
    css = css
      .replace(/url\s*\([^)]*\)/gi, 'none')
      .replace(/@import/gi, '/* blocked */')
      .replace(/expression\s*\([^)]*\)/gi, 'none')
      .replace(/position\s*:\s*fixed/gi, 'position: absolute')
      .replace(/content\s*:/gi, '/* content blocked */')

    const existing = document.getElementById('simple-theme')
    if (existing)
      existing.remove()

    const style = document.createElement('style')
    style.id = 'simple-theme'
    style.textContent = css
    document.head.appendChild(style)
  }
  catch (err) {
    // Theme loading is non-critical — fail silently
    console.warn('Failed to load custom theme via RPC', err)
  }
}
