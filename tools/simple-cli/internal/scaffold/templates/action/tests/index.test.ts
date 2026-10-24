import { describe, expect, it, vi } from 'vitest'
import { handler } from '../src/index'
import { createRequest } from './helpers'

// Importing src/index.ts registers the handler with the platform runtime. Under
// Node there is no runtime to register with: the SDK reports that from a
// promise nothing awaits, and Vitest fails the run over it even when every test
// passes. The tests call the exported handler directly, so registration is a
// no-op here. Keep this in every test file that imports src/index.ts.
vi.mock('@simpleplatform/sdk', async (importOriginal) => {
  const sdk = await importOriginal<typeof import('@simpleplatform/sdk')>()
  return { ...sdk, default: { ...sdk.default, Handle: vi.fn() } }
})

describe('{{.ActionName}}', () => {
  it('returns hello world message', async () => {
    const result = await handler(createRequest())
    expect(result).toEqual({ message: 'Hello, World!' })
  })

  // Add more tests here:
  //
  // it('handles custom payload', async () => {
  //   const result = await handler(createRequest({ payload: { name: 'Test' } }))
  //   expect(result.name).toBe('Test')
  // })
  //
  // it('uses user context', async () => {
  //   const result = await handler(createRequest({
  //     user: { id: 'USR001', email: 'test@example.com' }
  //   }))
  //   expect(result).toBeDefined()
  // })
})
