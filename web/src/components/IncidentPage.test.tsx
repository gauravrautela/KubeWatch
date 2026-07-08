import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { IncidentPage } from './IncidentPage'

afterEach(() => vi.unstubAllGlobals())

const suspect = {
  namespace: 'payments', kind: 'Deployment', name: 'checkout',
  score: 87, reasons: ['image change'], event_count: 1,
  latest_event_time: '2026-07-08T13:58:00Z',
  events: [{ event_id: 'ev-1', event_time: '2026-07-08T13:58:00Z', operation: 'UPDATE',
    classes: ['image'], actor_type: 'human', user_name: 'alice' }],
}

function stubApi(suspects: unknown[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      let body: unknown = {}
      if (url.includes('/api/facets')) {
        body = { clusters: ['c1', 'c2'], namespaces: [], kinds: ['ConfigMap', 'Deployment'], operations: [] }
      } else if (url.includes('/api/incident')) {
        const kinds = new URL(url, 'http://test').searchParams.get('kinds')?.split(',')
        const shown = kinds
          ? (suspects as { kind: string }[]).filter((s) => kinds.includes(s.kind))
          : suspects
        body = { incident: { cluster: 'c1', at: '2026-07-08T14:00:00Z', lookback: '1h0m0s' }, suspects: shown }
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) })
    }),
  )
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <MemoryRouter>
      <QueryClientProvider client={qc}>
        <IncidentPage />
      </QueryClientProvider>
    </MemoryRouter>,
  )
}

test('analyze fetches and renders ranked suspects', async () => {
  stubApi([suspect])
  renderPage()
  await userEvent.selectOptions(await screen.findByLabelText(/cluster/i), 'c1')
  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  expect(await screen.findByText('checkout')).toBeInTheDocument()
  expect(screen.getByText('87')).toBeInTheDocument()
  expect(screen.getByText('image change')).toBeInTheDocument()
})

test('empty result shows widen-lookback hint', async () => {
  stubApi([])
  renderPage()
  await userEvent.selectOptions(await screen.findByLabelText(/cluster/i), 'c1')
  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  expect(await screen.findByText(/no changes found/i)).toBeInTheDocument()
})

test('analyze disabled without a cluster', async () => {
  stubApi([])
  renderPage()
  await screen.findByLabelText(/cluster/i)
  expect(screen.getByRole('button', { name: /analyze/i })).toBeDisabled()
})

test('re-clicking analyze with unchanged inputs still refetches', async () => {
  stubApi([suspect])
  renderPage()
  await userEvent.selectOptions(await screen.findByLabelText(/cluster/i), 'c1')

  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  expect(await screen.findByText('checkout')).toBeInTheDocument()

  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  await screen.findByText('checkout')

  const incidentCalls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.filter(
    (call: unknown[]) => (call[0] as string).includes('/api/incident'),
  )
  expect(incidentCalls).toHaveLength(2)
})

test('facets load failure shows an inline error', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ ok: false, status: 500, json: () => Promise.resolve({ error: 'boom' }) }),
  )
  renderPage()
  expect(await screen.findByText(/failed to load clusters/i)).toBeInTheDocument()
})

const cmSuspect = {
  namespace: 'infra', kind: 'ConfigMap', name: 'app-config',
  score: 62, reasons: ['config change'], event_count: 1,
  latest_event_time: '2026-07-08T13:50:00Z',
  events: [{ event_id: 'ev-2', event_time: '2026-07-08T13:50:00Z', operation: 'UPDATE',
    classes: ['config-data'], actor_type: 'serviceaccount', user_name: 'system:serviceaccount:ci:d' }],
}

function lastIncidentCall(): string {
  const calls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls
    .map((call: unknown[]) => call[0] as string)
    .filter((url: string) => url.includes('/api/incident'))
  return calls[calls.length - 1] ?? ''
}

test('kind chips refetch server-side via the kinds param', async () => {
  stubApi([suspect, cmSuspect])
  renderPage()
  await userEvent.selectOptions(await screen.findByLabelText(/cluster/i), 'c1')
  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  expect(await screen.findByText('checkout')).toBeInTheDocument()
  expect(screen.getByText('app-config')).toBeInTheDocument()

  // Toggle Deployment on: a new request carries kinds=Deployment and only
  // the Deployment suspect comes back.
  await userEvent.click(screen.getByRole('button', { name: 'kind Deployment' }))
  await waitFor(() => expect(screen.queryByText('app-config')).not.toBeInTheDocument())
  expect(screen.getByText('checkout')).toBeInTheDocument()
  expect(decodeURIComponent(lastIncidentCall())).toContain('kinds=Deployment')
  expect(screen.getByRole('button', { name: 'kind Deployment' })).toHaveAttribute('aria-pressed', 'true')

  // Multi-select: also toggle ConfigMap — both kinds requested and shown.
  await userEvent.click(screen.getByRole('button', { name: 'kind ConfigMap' }))
  expect(await screen.findByText('app-config')).toBeInTheDocument()
  expect(decodeURIComponent(lastIncidentCall())).toContain('kinds=ConfigMap,Deployment')
})

test('operation chips add the operations param', async () => {
  stubApi([suspect])
  renderPage()
  await userEvent.selectOptions(await screen.findByLabelText(/cluster/i), 'c1')
  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  await screen.findByText('checkout')

  await userEvent.click(screen.getByRole('button', { name: 'operation DELETE' }))
  await waitFor(() =>
    expect(decodeURIComponent(lastIncidentCall())).toContain('operations=DELETE'),
  )
  expect(screen.getByRole('button', { name: 'operation DELETE' })).toHaveAttribute('aria-pressed', 'true')
})

test('filters reset on new analyze', async () => {
  stubApi([suspect])
  renderPage()
  await userEvent.selectOptions(await screen.findByLabelText(/cluster/i), 'c1')
  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  await screen.findByText('checkout')

  await userEvent.click(screen.getByRole('button', { name: 'kind Deployment' }))
  await waitFor(() => expect(lastIncidentCall()).toContain('kinds='))

  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  await screen.findByText('checkout')
  expect(screen.getByRole('button', { name: 'kind Deployment' })).toHaveAttribute('aria-pressed', 'false')
  expect(lastIncidentCall()).not.toContain('kinds=')
})

test('filtered-empty result explains the filters', async () => {
  stubApi([cmSuspect])
  renderPage()
  await userEvent.selectOptions(await screen.findByLabelText(/cluster/i), 'c1')
  await userEvent.click(screen.getByRole('button', { name: /analyze/i }))
  await screen.findByText('app-config')

  await userEvent.click(screen.getByRole('button', { name: 'kind Deployment' }))
  expect(await screen.findByText(/no changes match the current filters/i)).toBeInTheDocument()
})
