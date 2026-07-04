import { afterEach, beforeEach, test, expect, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { InfiniteData } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useLiveFeed } from './useLiveFeed'
import { eventsKey } from './api/hooks'
import type { Page, Row } from './types'

beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function row(id: string, time: string): Row {
  return {
    event_id: id, event_time: time, ingested_at: time, cluster: 'c1', source: 'webhook',
    operation: 'UPDATE', api_group: 'apps', api_version: 'v1', kind: 'Deployment',
    namespace: 'default', name: 'web', resource_uid: 'u', sub_resource: '',
    user_name: 'alice', user_groups: [], dry_run: false, diff: '[]',
  }
}

test('useLiveFeed prepends new since-rows without duplicating existing ones', async () => {
  const qc = new QueryClient()
  const filters = {}
  // seed the feed cache with one existing row
  qc.setQueryData<InfiniteData<Page>>(eventsKey(filters), {
    pageParams: [''],
    pages: [{ events: [row('old', '2026-07-01T10:00:00.000Z')], next_cursor: '' }],
  })

  // the since-poll returns the existing row (boundary dup) + one new row, ASC order
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: () =>
        Promise.resolve({
          events: [row('old', '2026-07-01T10:00:00.000Z'), row('new', '2026-07-01T10:00:05.000Z')],
          next_cursor: '',
        } satisfies Page),
    }),
  )

  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  )
  renderHook(() => useLiveFeed(filters), { wrapper })

  await vi.advanceTimersByTimeAsync(5000)

  const data = qc.getQueryData<InfiniteData<Page>>(eventsKey(filters))!
  const ids = data.pages[0].events.map((e) => e.event_id)
  expect(ids).toEqual(['new', 'old']) // new prepended, no duplicate 'old'
})
