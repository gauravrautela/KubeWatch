import { afterEach, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { FilterBar } from './FilterBar'

afterEach(() => vi.unstubAllGlobals())

function mockFacets() {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ clusters: ['c1', 'c2'], kinds: ['Deployment'], operations: ['UPDATE'] }),
    }),
  )
}

function Harness() {
  const [params] = useSearchParams()
  return (
    <>
      <FilterBar />
      <span data-testid="qs">{params.toString()}</span>
    </>
  )
}

function renderBar() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <Harness />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

test('typing a name writes it to the URL query string', async () => {
  mockFacets()
  renderBar()
  const input = screen.getByLabelText('name')
  await userEvent.type(input, 'web')
  expect(screen.getByTestId('qs').textContent).toContain('name=web')
})

test('selecting a cluster writes it to the URL', async () => {
  mockFacets()
  renderBar()
  await screen.findByRole('option', { name: 'c1' })
  await userEvent.selectOptions(screen.getByLabelText('cluster'), 'c1')
  expect(screen.getByTestId('qs').textContent).toContain('cluster=c1')
})
