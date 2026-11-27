/**
 * Tests for Record Behavior: {{.TableName}}
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
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

  // Each test starts with no recorded calls, whatever ran before it.
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // A new behavior has no rules yet: it changes no field and blocks no save.
  // When you add a rule to the script, replace the matching check below with
  // one that states the rule. Example:
  //   expect(mockContext.$form).toHaveBeenCalledWith('status')

  it('should change nothing on load', async () => {
    mockContext.$form.event = 'load'
    await behavior(mockContext)
    expect(mockContext.$form).not.toHaveBeenCalled()
  })

  it('should change nothing on update', async () => {
    mockContext.$form.event = 'update'
    await behavior(mockContext)
    expect(mockContext.$form).not.toHaveBeenCalled()
  })

  // A save is blocked by an error on the whole form or on one of its fields,
  // so neither may have been asked for.
  it('should block no save on submit', async () => {
    mockContext.$form.event = 'submit'
    await behavior(mockContext)
    expect(mockContext.$form.error).not.toHaveBeenCalled()
    expect(mockContext.$form).not.toHaveBeenCalled()
  })
})
