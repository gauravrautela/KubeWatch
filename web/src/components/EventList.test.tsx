import { afterEach, test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
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
    <MemoryRouter>
      <QueryClientProvider client={qc}>
        <EventList filters={{}} onSelect={onSelect} />
      </QueryClientProvider>
    </MemoryRouter>,
  )
  return onSelect
}

test('renders operation badge, resource, dry-run marker', async () => {
  renderList()
  expect(await screen.findByText('DELETE')).toBeInTheDocument()
  expect(screen.getByText('default')).toBeInTheDocument()
  expect(screen.getByText('web')).toBeInTheDocument()
  expect(screen.getByText('dry-run')).toBeInTheDocument()
  expect(screen.getByText('alice')).toBeInTheDocument()
})

test('row activates with keyboard', async () => {
  // Find the row via a cell's text: role-based queries would be ambiguous now
  // that each row also contains the (CSS-hidden) include/exclude buttons,
  // which jsdom cannot hide because Tailwind styles are not loaded in tests.
  const onSelect = renderList()
  const rowEl = (await screen.findByText('dry-run')).closest('tr')!
  rowEl.focus()
  await userEvent.keyboard('{Enter}')
  expect(onSelect).toHaveBeenCalledWith('1')
})

test('cell values expose include/exclude filter buttons', async () => {
  renderList()
  await screen.findByText('DELETE')
  for (const name of [
    'include kind Deployment', 'exclude kind Deployment',
    'include namespace default', 'exclude namespace default',
    'include name web', 'exclude name web',
    'include user alice', 'exclude user alice',
    'include cluster c1', 'exclude cluster c1',
    'include operation DELETE', 'exclude operation DELETE',
  ]) {
    expect(screen.getByRole('button', { name })).toBeInTheDocument()
  }
})

test('include button click filters without opening the detail row', async () => {
  const onSelect = renderList()
  await screen.findByText('DELETE')
  await userEvent.click(screen.getByRole('button', { name: 'include user alice' }))
  expect(onSelect).not.toHaveBeenCalled()
})
