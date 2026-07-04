import { afterEach, test, expect, vi } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useFacets, useEventsFeed, eventsKey } from './hooks'

afterEach(() => vi.unstubAllGlobals())

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  )
}

function mockFetch(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(body) }))
}

test('eventsKey is stable for equal filters', () => {
  expect(eventsKey({ cluster: 'c1' })).toEqual(eventsKey({ cluster: 'c1' }))
})

test('useFacets fetches facets', async () => {
  mockFetch({ clusters: ['c1'], kinds: [], operations: [] })
  const { result } = renderHook(() => useFacets(), { wrapper: wrapper() })
  await waitFor(() => expect(result.current.isSuccess).toBe(true))
  expect(result.current.data?.clusters).toEqual(['c1'])
})

test('useEventsFeed loads the first page', async () => {
  mockFetch({ events: [{ event_id: '1' }], next_cursor: '' })
  const { result } = renderHook(() => useEventsFeed({}), { wrapper: wrapper() })
  await waitFor(() => expect(result.current.isSuccess).toBe(true))
  expect(result.current.data?.pages[0].events[0].event_id).toBe('1')
})
