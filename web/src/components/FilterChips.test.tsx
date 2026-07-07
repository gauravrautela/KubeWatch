import { test, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { FilterChips } from './FilterChips'

function Harness() {
  const [params] = useSearchParams()
  return (
    <>
      <FilterChips />
      <span data-testid="qs">{decodeURIComponent(params.toString())}</span>
    </>
  )
}

function renderChips(initial: string) {
  render(
    <MemoryRouter initialEntries={[initial]}>
      <Harness />
    </MemoryRouter>,
  )
}

test('renders nothing when no filters are active', () => {
  renderChips('/')
  expect(screen.queryByRole('button', { name: /clear all/i })).not.toBeInTheDocument()
})

test('shows a chip per filter and removes one on click', async () => {
  renderChips('/?cluster=c1&exclude_kinds=Lease,Endpoints')
  expect(screen.getByText('cluster: c1')).toBeInTheDocument()
  expect(screen.getByText('not kind: Lease')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: 'remove not kind: Lease' }))
  expect(screen.getByTestId('qs').textContent).toContain('exclude_kinds=Endpoints')
  expect(screen.getByTestId('qs').textContent).not.toContain('Lease')
})

test('clear all removes every filter', async () => {
  renderChips('/?cluster=c1&q=ali&exclude_namespaces=kube-system')
  await userEvent.click(screen.getByRole('button', { name: /clear all/i }))
  expect(screen.getByTestId('qs').textContent).toBe('')
})
