/**
 * Tests for Record Behavior: {{.TableName}}
 */
import { describe, expect, it, vi } from 'vitest'
import behavior from './{{.TableName}}.js'

describe('record Behavior: {{.TableName}}', () => {
  // Mock Context
  const mockContext = {
    $ai: {},
    $db: {
      query: vi.fn(),
    },
    $form: Object.assign(
      vi.fn(_field => ({
        editable: vi.fn(),
        error: vi.fn(),
        set: vi.fn(),
        value: vi.fn(),
        visible: vi.fn(),
      })),
      {
        error: vi.fn(),
        event: '',
        record: vi.fn(() => ({})),
        updated: vi.fn(),
      },
    ),
    $user: {
      id: 'USR000001',
    },
  }

  it('should handle load event', async () => {
    mockContext.$form.event = 'load'
    await behavior(mockContext)
    // Example assertion: verify a field was updated
    expect(mockContext.$form).toHaveBeenCalled()
  })

  it('should handle update event', async () => {
    mockContext.$form.event = 'update'
    await behavior(mockContext)
    expect(mockContext.$form).toHaveBeenCalled()
  })

  it('should handle submit event', async () => {
    mockContext.$form.event = 'submit'
    await behavior(mockContext)
    expect(mockContext.$form).toHaveBeenCalled()
  })
})
