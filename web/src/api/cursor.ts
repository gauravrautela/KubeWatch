import type { Row } from '../types'

// encodeCursor reproduces the server's EncodeCursor: base64url-no-pad of
// "<event_time as unix nanoseconds>:<event_id>". event_time is DateTime64(3)
// (millisecond precision), which a JS Date represents exactly.
export function encodeCursor(row: Pick<Row, 'event_time' | 'event_id'>): string {
  const ms = new Date(row.event_time).getTime()
  const ns = BigInt(ms) * 1_000_000n
  const raw = `${ns}:${row.event_id}`
  return btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}
