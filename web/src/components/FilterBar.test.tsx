import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
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

function Harness() {
  const [params] = useSearchParams()
  return (
    <>
      <FilterBar />
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
