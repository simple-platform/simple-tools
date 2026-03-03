import { render, screen } from '@testing-library/react'
import React from 'react'
import { describe, expect, it } from 'vitest'
import App from '../src/App'

describe('app', () => {
  it('renders the display name', () => {
    render(<App />)
    expect(screen.getByText('{{.DisplayName}}')).toBeDefined()
  })
})
