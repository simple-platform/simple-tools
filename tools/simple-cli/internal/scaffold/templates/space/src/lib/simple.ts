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
const pendingRequests = new Map<string, { resolve: (value: any) => void, reject: (reason?: any) => void }>()
const requestQueue: any[] = [] // Queue requests before port is ready

// 1. Wait for Parent to give us our dedicated MessagePort
window.addEventListener('message', (event) => {
  if (event.data?.type === 'INIT_RPC' && event.ports[0]) {
    rpcPort = event.ports[0]

    // Listen for responses matching our UUIDs
    rpcPort.onmessage = (e) => {
      if (e.data?.type === 'GRAPHQL_RESPONSE') {
        const { data, error, id } = e.data
        const req = pendingRequests.get(id)
        if (req) {
          if (error)
            req.reject(new Error(error))
          else req.resolve(data)
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
  window.parent.postMessage({ type: 'SPACE_READY' }, '*')
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
    pendingRequests.set(id, { reject, resolve })

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
 * Loads tenant-specific theme overrides from the settings table via RPC.
 * Call this once on app mount (e.g., in main.tsx).
 */
export async function loadTheme(): Promise<void> {
  try {
    const data = await query<{ theme: { value: string }[] }>(GET_THEME)

    const css = data?.theme?.[0]?.value
    if (!css || typeof css !== 'string' || !css.includes('--'))
      return

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
