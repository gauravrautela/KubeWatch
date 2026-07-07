import { test, expect } from 'vitest'
import { presetFrom, pickBucket, bucketRange } from './timeRange'

const now = new Date('2026-07-07T12:00:00.000Z')

test('presetFrom returns RFC3339 now-minus-duration', () => {
  expect(presetFrom('1h', now)).toBe('2026-07-07T11:00:00.000Z')
  expect(presetFrom('7d', now)).toBe('2026-06-30T12:00:00.000Z')
})

test('pickBucket: no from => day; <=2h => minute; <=3d => hour; else day', () => {
  expect(pickBucket(undefined, undefined, now)).toBe('day')
  expect(pickBucket('2026-07-07T11:00:00.000Z', undefined, now)).toBe('minute')
  expect(pickBucket('2026-07-06T12:00:00.000Z', undefined, now)).toBe('hour')
  expect(pickBucket('2026-06-01T00:00:00.000Z', undefined, now)).toBe('day')
  expect(pickBucket('2026-07-01T00:00:00.000Z', '2026-07-01T01:00:00.000Z', now)).toBe('minute')
})

test('bucketRange spans exactly one bucket', () => {
  expect(bucketRange('2026-07-07T11:00:00.000Z', 'hour')).toEqual({
    from: '2026-07-07T11:00:00.000Z',
    to: '2026-07-07T12:00:00.000Z',
  })
  expect(bucketRange('2026-07-07T11:00:00.000Z', 'minute').to).toBe('2026-07-07T11:01:00.000Z')
})
