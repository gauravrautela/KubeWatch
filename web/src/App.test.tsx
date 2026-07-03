import { render, screen } from '@testing-library/react'
import { test, expect } from 'vitest'
import { App } from './App'

test('renders the dashboard title', () => {
  render(<App />)
  expect(screen.getByText('KubeWatch Dashboard')).toBeInTheDocument()
})
