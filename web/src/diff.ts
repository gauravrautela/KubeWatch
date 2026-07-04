import type { DiffChange } from './types'

export function parseDiff(diff: string): DiffChange[] {
  if (!diff) return []
  try {
    const parsed = JSON.parse(diff)
    return Array.isArray(parsed) ? (parsed as DiffChange[]) : []
  } catch {
    return []
  }
}

export function formatValue(v: unknown): string {
  if (v === undefined) return '∅'
  if (v === null) return 'null'
  if (typeof v === 'string') return `"${v}"`
  if (typeof v === 'object') return JSON.stringify(v)
  return String(v)
}
