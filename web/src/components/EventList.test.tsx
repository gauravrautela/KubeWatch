import { afterEach, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { EventList } from './EventList'

afterEach(() => vi.unstubAllGlobals())

const row = {
  event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
  cluster: 'c1', source: 'webhook', operation: 'DELETE', api_group: 'apps', api_version: 'v1',
  kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
  user_name: 'alice', user_groups: [], dry_run: true, diff: '[]',
}

function renderList(onSelect = vi.fn()) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true, status: 200,
      json: () => Promise.resolve({ events: [row], next_cursor: '' }),
    }),
  )
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <EventList filters={{}} onSelect={onSelect} />
    </QueryClientProvider>,
  )
  return onSelect
}

test('renders operation badge, resource, dry-run marker', async () => {
  renderList()
  expect(await screen.findByText('DELETE')).toBeInTheDocument()
  expect(screen.getByText('default/web')).toBeInTheDocument()
  expect(screen.getByText('dry-run')).toBeInTheDocument()
  expect(screen.getByText('alice')).toBeInTheDocument()
})

test('row activates with keyboard', async () => {
  const onSelect = renderList()
  const btn = await screen.findByRole('button', { name: /default\/web/ })
  btn.focus()
  await userEvent.keyboard('{Enter}')
  expect(onSelect).toHaveBeenCalledWith('1')
})
