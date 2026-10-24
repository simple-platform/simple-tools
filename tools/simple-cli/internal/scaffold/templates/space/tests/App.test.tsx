// @vitest-environment happy-dom
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../src/App'

// A Space is served in a frame, and it answers to the page that framed it:
// App.tsx takes the host origin from document.referrer. The test environment
// has no framing page, so one is named before App.tsx is evaluated.
vi.hoisted(() => {
  Object.defineProperty(document, 'referrer', { configurable: true, value: 'https://tenant.example/' })
})

// Mock the simple SDK so tests don't need a real RPC connection
vi.mock('@simpleplatform/sdk/space', () => ({
  connect: vi.fn().mockResolvedValue({
    context: { kind: 'standalone' },
    data: {
      query: vi.fn().mockResolvedValue({
        application_tables: [{ id: 'TBL000001' }],
        applications: [
          { display_name: 'Test App One', id: '1', version: '1.0.0' },
          { display_name: 'Test App Two', id: '2', version: '2.3.1' },
        ],
      }),
    },
  }),
}))

vi.mock('@simpleplatform/ui-kit/react', () => ({
  ListView: (props: any) => (
    <div
      data-preset-kind={props.preset?.kind}
      data-source-table-id={props.source?.tableId}
      data-testid="managed-list-view"
    >
      <span>{props.preset?.title}</span>
    </div>
  ),
}))

afterEach(cleanup)

describe('app', () => {
  it('renders the display name', () => {
    render(<App />)
    expect(screen.getByText('{{.DisplayName}}')).toBeDefined()
  })

  it('shows loading state initially', () => {
    render(<App />)
    expect(screen.getByText('Loading applications…')).toBeDefined()
  })

  it('renders application data from GraphQL', async () => {
    render(<App />)
    await waitFor(() => {
      expect(screen.getByText('Test App One')).toBeDefined()
      expect(screen.getByText('Test App Two')).toBeDefined()
    })
  })

  it('renders application versions', async () => {
    render(<App />)
    await waitFor(() => {
      expect(screen.getByText('1.0.0')).toBeDefined()
      expect(screen.getByText('2.3.1')).toBeDefined()
    })
  })

  it('shows the host-provided Space context', async () => {
    render(<App />)
    await waitFor(() => expect(screen.getByText('Space context: standalone')).toBeDefined())
  })

  it('renders the database-backed ListView component', async () => {
    render(<App />)
    await waitFor(() => {
      const listView = screen.getByTestId('managed-list-view')
      expect(listView).toBeDefined()
      expect(listView.getAttribute('data-preset-kind')).toBe('list')
      expect(listView.getAttribute('data-source-table-id')).toBe('TBL000001')
      expect(screen.getByText('Applications')).toBeDefined()
    })
  })
})
