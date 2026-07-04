import { test, expect } from 'vitest'
import { parseDiff, formatValue } from './diff'

test('parseDiff parses a valid diff array', () => {
  const changes = parseDiff('[{"path":"spec.replicas","op":"replace","old":2,"new":3}]')
  expect(changes).toHaveLength(1)
  expect(changes[0]).toMatchObject({ path: 'spec.replicas', op: 'replace', old: 2, new: 3 })
})

test('parseDiff returns [] for empty or invalid input', () => {
  expect(parseDiff('')).toEqual([])
  expect(parseDiff('not json')).toEqual([])
})

test('formatValue renders scalars and objects readably', () => {
  expect(formatValue(3)).toBe('3')
  expect(formatValue('x')).toBe('"x"')
  expect(formatValue(undefined)).toBe('∅')
  expect(formatValue({ a: 1 })).toBe('{"a":1}')
})
