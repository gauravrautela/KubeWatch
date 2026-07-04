import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EventDetail } from './EventDetail'

afterEach(() => vi.unstubAllGlobals())

const detail = {
  event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
  cluster: 'c1', source: 'webhook', operation: 'UPDATE', api_group: 'apps', api_version: 'v1',
  kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
  user_name: 'alice', user_groups: [], dry_run: false,
  diff: '[{"path":"spec.replicas","op":"replace","old":2,"new":3}]',
  old_object: '{"spec":{"replicas":2}}', new_object: '{"spec":{"replicas":3}}',
  user_uid: 'uid', user_agent: '',
}

function renderDetail() {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(detail) }))
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <EventDetail id="1" />
    </QueryClientProvider>,
  )
}

test('renders the structured diff by default', async () => {
  renderDetail()
  await waitFor(() => expect(screen.getByText('spec.replicas')).toBeInTheDocument())
  expect(screen.getByText(/replace/i)).toBeInTheDocument()
  expect(screen.getByText('alice')).toBeInTheDocument()
})

test('View raw toggles to side-by-side objects', async () => {
  renderDetail()
  await screen.findByText('spec.replicas')
  await userEvent.click(screen.getByRole('button', { name: /view raw/i }))
  expect(screen.getByLabelText('before')).toHaveTextContent('"replicas": 2')
  expect(screen.getByLabelText('after')).toHaveTextContent('"replicas": 3')
})
