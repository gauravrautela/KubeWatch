import { test, expect } from 'vitest'
import { encodeCursor } from './cursor'

// Decodes the base64url-no-pad cursor back to "ns:uuid" for verification.
function decode(cur: string): string {
  const b64 = cur.replace(/-/g, '+').replace(/_/g, '/')
  return atob(b64)
}

test('encodeCursor renders base64url-no-pad of "<unixNanos>:<uuid>"', () => {
  const cur = encodeCursor({ event_time: '2026-07-01T10:00:00.000Z', event_id: 'abc-123' })
  expect(cur).not.toMatch(/[+/=]/) // url-safe, no padding
  const ms = new Date('2026-07-01T10:00:00.000Z').getTime()
  expect(decode(cur)).toBe(`${BigInt(ms) * 1_000_000n}:abc-123`)
})
