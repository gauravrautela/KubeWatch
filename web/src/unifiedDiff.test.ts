import { test, expect } from 'vitest'
import { buildUnifiedDiff, type Segment } from './unifiedDiff'

function flat(segments: Segment[]): string[] {
  return segments.flatMap((s) => s.lines.map((l) => `${s.kind === 'fold' ? 'F' : l.type[0]}:${l.text}`))
}

test('replace produces del+add between context', () => {
  const segs = buildUnifiedDiff('{"a":1}', '{"a":2}')
  expect(flat(segs)).toEqual(['c:{', 'd:  "a": 1', 'a:  "a": 2', 'c:}'])
})

test('CREATE (empty old) is all additions', () => {
  const segs = buildUnifiedDiff('', '{"a":1}')
  const lines = flat(segs)
  expect(lines.length).toBeGreaterThan(0)
  expect(lines.every((l) => l.startsWith('a:'))).toBe(true)
})

test('DELETE (empty new) is all removals', () => {
  const segs = buildUnifiedDiff('{"a":1}', '')
  expect(flat(segs).every((l) => l.startsWith('d:'))).toBe(true)
})

test('long unchanged runs fold to 3 lines of context', () => {
  const items = Array.from({ length: 30 }, (_, i) => i)
  const oldObj = JSON.stringify({ items, x: 1 })
  const newObj = JSON.stringify({ items, x: 2 })
  const segs = buildUnifiedDiff(oldObj, newObj)
  const fold = segs.find((s) => s.kind === 'fold')
  expect(fold).toBeDefined()
  expect(fold!.lines.length).toBeGreaterThan(0)
  expect(fold!.lines.every((l) => l.type === 'ctx')).toBe(true)
})

test('malformed JSON falls back to raw text diff', () => {
  const segs = buildUnifiedDiff('not json {', 'not json }')
  const lines = flat(segs)
  expect(lines).toContain('d:not json {')
  expect(lines).toContain('a:not json }')
})
