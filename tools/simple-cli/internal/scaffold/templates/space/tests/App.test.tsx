// @vitest-environment happy-dom
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../src/App'

// Mock the simple SDK so tests don't need a real RPC connection
vi.mock('../src/lib/simple', () => ({
  mutate: vi.fn(),
  query: vi.fn().mockResolvedValue({
    applications: [
      { display_name: 'Test App One', id: '1', version: '1.0.0' },
      { display_name: 'Test App Two', id: '2', version: '2.3.1' },
    ],
  }),
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
})
