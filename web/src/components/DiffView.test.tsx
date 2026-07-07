import { test, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DiffView } from './DiffView'

const items = Array.from({ length: 30 }, (_, i) => i)
const oldObj = JSON.stringify({ items, x: 1 })
const newObj = JSON.stringify({ items, x: 2 })

test('renders changed lines and a fold expander; expanding reveals lines', async () => {
  render(<DiffView oldJson={oldObj} newJson={newObj} />)
  expect(screen.getByText(/"x": 1/)).toBeInTheDocument()
  expect(screen.getByText(/"x": 2/)).toBeInTheDocument()
  const expander = screen.getByRole('button', { name: /unchanged lines/ })
  const before = document.querySelectorAll('[data-line]').length
  await userEvent.click(expander)
  expect(document.querySelectorAll('[data-line]').length).toBeGreaterThan(before)
})
