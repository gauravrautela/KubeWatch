import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { FilterBar } from './FilterBar'
import { useFilters } from '../useFilters'

afterEach(() => vi.unstubAllGlobals())

function mockFacets() {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () =>
        Promise.resolve({
          clusters: ['c1', 'c2'],
          namespaces: ['default', 'kube-system'],
          kinds: ['Deployment', 'Lease'],
          operations: ['UPDATE'],
        }),
    }),
  )
}

// ExternalControls simulates state changes that originate outside FilterBar,
// e.g. a histogram bar-click zoom (sets both from+to) or "Clear all" /
// removing a chip (clears from+to).
function ExternalControls() {
  const { setFilters } = useFilters()
  return (
    <>
      <button onClick={() => setFilters({ from: '2026-07-07T09:00:00.000Z', to: '2026-07-07T09:05:00.000Z' })}>
        zoom
      </button>
      <button onClick={() => setFilters({ from: '', to: '' })}>clear-bounds</button>
    </>
  )
}

function Harness() {
  const [params] = useSearchParams()
  return (
    <>
      <FilterBar />
      <ExternalControls />
      <span data-testid="qs">{decodeURIComponent(params.toString())}</span>
    </>
  )
}

function renderBar(initial = '/') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Harness />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const qs = () => screen.getByTestId('qs').textContent ?? ''

test('search box debounces into the q param', async () => {
  mockFacets()
  renderBar()
  await userEvent.type(screen.getByLabelText('search'), 'ali')
  await waitFor(() => expect(qs()).toContain('q=ali'))
})

test('selecting a namespace writes it to the URL', async () => {
  mockFacets()
  renderBar()
  await screen.findByRole('option', { name: 'kube-system' })
  await userEvent.selectOptions(screen.getByLabelText('namespace'), 'kube-system')
  expect(qs()).toContain('namespace=kube-system')
})

test('picking a preset writes an RFC3339 from and no to', async () => {
  mockFacets()
  renderBar()
  await userEvent.selectOptions(screen.getByLabelText('time range'), '1h')
  expect(qs()).toMatch(/from=\d{4}-\d{2}-\d{2}T[\d:.]+Z/)
  expect(qs()).not.toContain('to=')
})

test('custom range applies both bounds as RFC3339', async () => {
  mockFacets()
  renderBar()
  await userEvent.selectOptions(screen.getByLabelText('time range'), 'custom')
  await userEvent.type(screen.getByLabelText('from'), '2026-07-07T10:00')
  await userEvent.type(screen.getByLabelText('to'), '2026-07-07T11:00')
  await userEvent.click(screen.getByRole('button', { name: 'Apply' }))
  expect(qs()).toMatch(/from=[^&]+Z/)
  expect(qs()).toMatch(/to=[^&]+Z/)
})

test('ignore select accumulates comma-separated excludes', async () => {
  mockFacets()
  renderBar()
  await screen.findByRole('option', { name: 'Lease' })
  await userEvent.selectOptions(screen.getByLabelText('ignore'), 'kind:Lease')
  expect(qs()).toContain('exclude_kinds=Lease')
  await userEvent.selectOptions(screen.getByLabelText('ignore'), 'ns:kube-system')
  expect(qs()).toContain('exclude_namespaces=kube-system')
})

test('loading with both from and to bounds shows custom and reveals custom inputs', async () => {
  mockFacets()
  renderBar('/?from=2026-07-07T10:00:00.000Z&to=2026-07-07T11:00:00.000Z')
  expect(screen.getByLabelText('time range')).toHaveValue('custom')
  expect(screen.getByLabelText('from')).toBeInTheDocument()
  expect(screen.getByLabelText('to')).toBeInTheDocument()
})

test('an external from/to change (e.g. histogram zoom) flips the select to custom', async () => {
  mockFacets()
  renderBar()
  await userEvent.selectOptions(screen.getByLabelText('time range'), '1h')
  expect(screen.getByLabelText('time range')).toHaveValue('1h')

  await userEvent.click(screen.getByRole('button', { name: 'zoom' }))

  await waitFor(() => expect(screen.getByLabelText('time range')).toHaveValue('custom'))
  expect(screen.getByLabelText('from')).toBeInTheDocument()
})

test('clearing from/to externally resets the select to "all" and hides custom inputs', async () => {
  mockFacets()
  renderBar('/?from=2026-07-07T10:00:00.000Z&to=2026-07-07T11:00:00.000Z')
  expect(screen.getByLabelText('time range')).toHaveValue('custom')

  await userEvent.click(screen.getByRole('button', { name: 'clear-bounds' }))

  await waitFor(() => expect(screen.getByLabelText('time range')).toHaveValue(''))
  expect(screen.queryByLabelText('from')).not.toBeInTheDocument()
})
