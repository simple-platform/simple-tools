import type { Request } from '@simpleplatform/sdk'

/**
 * Options for creating a mock request.
 */
interface RequestOptions {
  user?: Record<string, any>
  tenant?: Record<string, any>
  context?: Record<string, any>
  headers?: Record<string, any>
  data?: string
  payload?: any
}

/**
 * Creates a mock request for testing action handlers.
 * Override any properties as needed for specific test cases.
 *
 * @example
 * // Basic request with defaults
 * const req = createRequest()
 *
 * // Request with custom payload
 * const req = createRequest({ payload: { name: 'Test' } })
 *
 * // Request with user context
 * const req = createRequest({ user: { id: 'USR001', email: 'test@example.com' } })
 */
export function createRequest(options: RequestOptions = {}): Request {
  return {
    context: {
      tenant: options.tenant ?? {},
      user: options.user ?? {},
      ...options.context,
    },
    data: () => options.data ?? '',
    headers: options.headers ?? {},
    parse: <T>() => (options.payload ?? {}) as T,
  } as Request
}
