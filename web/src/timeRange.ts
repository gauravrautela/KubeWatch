export const PRESETS = {
  '15m': 15 * 60_000,
  '1h': 3_600_000,
  '6h': 6 * 3_600_000,
  '24h': 24 * 3_600_000,
  '7d': 7 * 86_400_000,
} as const

export type Preset = keyof typeof PRESETS

// presetFrom returns the RFC3339 `from` bound for a live preset window.
// `to` is intentionally left unset by callers so the feed stays live.
export function presetFrom(p: Preset, now: Date = new Date()): string {
  return new Date(now.getTime() - PRESETS[p]).toISOString()
}

export type BucketUnit = 'minute' | 'hour' | 'day'

const BUCKET_MS: Record<BucketUnit, number> = {
  minute: 60_000,
  hour: 3_600_000,
  day: 86_400_000,
}

// pickBucket chooses the histogram granularity for the active range:
// <=2h => minute, <=3d => hour, else (or unbounded) => day.
export function pickBucket(from?: string, to?: string, now: Date = new Date()): BucketUnit {
  if (!from) return 'day'
  const span = (to ? new Date(to) : now).getTime() - new Date(from).getTime()
  if (span <= 2 * 3_600_000) return 'minute'
  if (span <= 3 * 86_400_000) return 'hour'
  return 'day'
}

// bucketRange is the [from, to] window of one histogram bucket (for bar-click zoom).
export function bucketRange(bucketStartIso: string, unit: BucketUnit): { from: string; to: string } {
  const start = new Date(bucketStartIso)
  return {
    from: start.toISOString(),
    to: new Date(start.getTime() + BUCKET_MS[unit]).toISOString(),
  }
}
