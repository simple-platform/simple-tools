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
const requestQueue: any[] = [] // Queue requests before port is ready

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
    }

    // Flush any requests that were queued before the port was ready
    while (requestQueue.length > 0) {
      rpcPort.postMessage(requestQueue.shift())
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
      requestQueue.push(payload)
    }
  })
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/** Execute a GraphQL query. Returns the `data` object. */
export const query = executeRpcGraphQL

/** Execute a GraphQL mutation. Returns the `data` object. */
export const mutate = executeRpcGraphQL

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
      requestQueue.push(payload)
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
