import { describe, expect, it } from 'vitest'
import { handler } from '../index'
import { createRequest } from './helpers'

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
