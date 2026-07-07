import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EventDetailPage } from './EventDetail'

afterEach(() => vi.unstubAllGlobals())

const detail = {
  event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
  cluster: 'c1', source: 'webhook', operation: 'UPDATE', api_group: 'apps', api_version: 'v1',
  kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
  user_name: 'alice', user_groups: ['system:masters'], dry_run: false,
  diff: '[{"path":"spec.replicas","op":"replace","old":2,"new":3}]',
  old_object: '{"spec":{"replicas":2}}', new_object: '{"spec":{"replicas":3}}',
  user_uid: 'uid', user_agent: 'kubectl/v1.30',
}

function renderPage() {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(detail) }))
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/events/1?cluster=c1']}>
        <Routes>
          <Route path="/events/:id" element={<EventDetailPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

test('renders metadata and a unified diff', async () => {
  renderPage()
  await waitFor(() => expect(screen.getByText('alice')).toBeInTheDocument())
  expect(screen.getByText('UPDATE')).toBeInTheDocument()
  expect(screen.getByText(/default\/web/)).toBeInTheDocument()
  expect(screen.getByText(/"replicas": 2/)).toBeInTheDocument() // del line
  expect(screen.getByText(/"replicas": 3/)).toBeInTheDocument() // add line
})

test('breadcrumb preserves the query string', async () => {
  renderPage()
  await screen.findByText('alice')
  const back = screen.getByRole('link', { name: /events/i })
  expect(back).toHaveAttribute('href', '/?cluster=c1')
})
