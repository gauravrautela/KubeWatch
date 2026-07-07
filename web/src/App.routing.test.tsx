import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from './App'

afterEach(() => vi.unstubAllGlobals())

function stubApi() {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockImplementation((url: string) => {
      let body: unknown = {}
      if (url.includes('/api/facets')) {
        body = { clusters: ['c1'], namespaces: ['default'], kinds: ['Deployment'], operations: ['UPDATE'] }
      } else if (url.includes('/api/activity')) body = []
      else if (/\/api\/events\/[^?]/.test(url)) {
        body = {
          event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
          cluster: 'c1', source: 'webhook', operation: 'UPDATE', api_group: 'apps', api_version: 'v1',
          kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
          user_name: 'alice', user_groups: [], dry_run: false,
          diff: '[{"path":"spec.replicas","op":"replace","old":2,"new":3}]',
          old_object: '{"spec":{"replicas":2}}', new_object: '{"spec":{"replicas":3}}',
          user_uid: '', user_agent: '',
        }
      } else {
        body = {
          events: [{
            event_id: '1', event_time: '2026-07-01T10:00:00Z', ingested_at: '2026-07-01T10:00:01Z',
            cluster: 'c1', source: 'webhook', operation: 'UPDATE', api_group: 'apps', api_version: 'v1',
            kind: 'Deployment', namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
            user_name: 'alice', user_groups: [], dry_run: false, diff: '[]',
          }],
          next_cursor: '',
        }
      }
      return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) })
    }),
  )
}

test('clicking a feed row opens the full detail page with a unified diff', async () => {
  stubApi()
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/']}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )

  // 'default' is ambiguous here (also appears as a namespace <option>), so
  // click the resource name, which is unique.
  const row = await screen.findByText('web')
  await userEvent.click(row)
  await waitFor(() => expect(screen.getByText(/"replicas": 3/)).toBeInTheDocument())
  // dashboard content is replaced by the page
  expect(screen.queryByLabelText('search')).not.toBeInTheDocument()
})
