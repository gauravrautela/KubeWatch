import { afterEach, test, expect, vi } from 'vitest'
import { ApiError, fetchActivity, fetchEvents } from './client'

afterEach(() => vi.unstubAllGlobals())

function mockFetch(body: unknown, ok = true, status = 200) {
  const f = vi.fn().mockResolvedValue({ ok, status, json: () => Promise.resolve(body) })
  vi.stubGlobal('fetch', f)
  return f
}

test('fetchEvents builds URL with filters, cursor, and limit', async () => {
  const f = mockFetch({ events: [], next_cursor: '' })
  await fetchEvents({ cluster: 'c1', name: 'web' }, { cursor: 'abc', limit: 50 })
  const url = f.mock.calls[0][0] as string
  expect(url).toContain('/api/events?')
  expect(url).toContain('cluster=c1')
  expect(url).toContain('name=web')
  expect(url).toContain('cursor=abc')
  expect(url).toContain('limit=50')
})

test('fetchEvents omits empty filter values', async () => {
  const f = mockFetch({ events: [], next_cursor: '' })
  await fetchEvents({ cluster: '', name: 'web' })
  const url = f.mock.calls[0][0] as string
  expect(url).not.toContain('cluster=')
  expect(url).toContain('name=web')
})

test('fetchActivity sets the bucket param', async () => {
  const f = mockFetch([])
  await fetchActivity({}, 'hour')
  expect(f.mock.calls[0][0]).toContain('bucket=hour')
})

test('non-2xx throws ApiError carrying the server error message', async () => {
  mockFetch({ error: 'invalid cursor' }, false, 400)
  await expect(fetchEvents({})).rejects.toMatchObject({ status: 400, message: 'invalid cursor' })
  mockFetch({ error: 'x' }, false, 400)
  const err = await fetchEvents({}).catch((e) => e)
  expect(err).toBeInstanceOf(ApiError)
})
