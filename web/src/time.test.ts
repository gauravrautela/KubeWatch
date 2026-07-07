import { test, expect } from 'vitest'
import { timeAgo } from './time'

const now = new Date('2026-07-07T12:00:00Z')

test('timeAgo formats seconds/minutes/hours/days', () => {
  expect(timeAgo('2026-07-07T11:59:30Z', now)).toBe('30s ago')
  expect(timeAgo('2026-07-07T11:45:00Z', now)).toBe('15m ago')
  expect(timeAgo('2026-07-07T09:00:00Z', now)).toBe('3h ago')
  expect(timeAgo('2026-07-01T12:00:00Z', now)).toBe('6d ago')
  expect(timeAgo('2026-07-07T12:00:05Z', now)).toBe('0s ago') // clock skew clamps to 0
})
