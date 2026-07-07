import { test, expect } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { useFilters, splitList, addToList, removeFromList } from './useFilters'

function wrapper({ children }: { children: React.ReactNode }) {
  return <MemoryRouter initialEntries={['/?cluster=c1']}>{children}</MemoryRouter>
}

test('filters keeps a stable reference across re-renders when URL params are unchanged', () => {
  const { result, rerender } = renderHook(() => useFilters(), { wrapper })

  const firstFilters = result.current.filters
  const firstSetFilter = result.current.setFilter

  rerender()

  expect(result.current.filters).toBe(firstFilters)
  expect(result.current.setFilter).toBe(firstSetFilter)
})

test('filters gets a new reference when a URL param changes', () => {
  const { result } = renderHook(() => useFilters(), { wrapper })

  const firstFilters = result.current.filters
  expect(firstFilters.cluster).toBe('c1')

  act(() => {
    result.current.setFilter('cluster', 'c2')
  })

  expect(result.current.filters).not.toBe(firstFilters)
  expect(result.current.filters.cluster).toBe('c2')
})

test('setFilters writes several keys in one update; empty deletes', () => {
  const { result } = renderHook(() => useFilters(), { wrapper })
  act(() => {
    result.current.setFilters({ from: '2026-07-07T00:00:00.000Z', to: '2026-07-07T01:00:00.000Z' })
  })
  expect(result.current.filters.from).toBe('2026-07-07T00:00:00.000Z')
  expect(result.current.filters.to).toBe('2026-07-07T01:00:00.000Z')
  act(() => {
    result.current.setFilters({ from: '', to: '' })
  })
  expect(result.current.filters.from).toBeUndefined()
  expect(result.current.filters.to).toBeUndefined()
})

test('clearFilters removes every filter key', () => {
  const { result } = renderHook(() => useFilters(), { wrapper }) // starts with cluster=c1
  act(() => {
    result.current.setFilter('exclude_kinds', 'Lease,Endpoints')
  })
  act(() => {
    result.current.clearFilters()
  })
  expect(result.current.filters).toEqual({})
})

test('csv list helpers add, dedupe, and remove', () => {
  expect(splitList(undefined)).toEqual([])
  expect(splitList('a,b')).toEqual(['a', 'b'])
  expect(addToList(undefined, 'Lease')).toBe('Lease')
  expect(addToList('Lease', 'Endpoints')).toBe('Lease,Endpoints')
  expect(addToList('Lease', 'Lease')).toBe('Lease')
  expect(removeFromList('Lease,Endpoints', 'Lease')).toBe('Endpoints')
  expect(removeFromList('Lease', 'Lease')).toBe('')
})

test('tracks and clears the new exclude keys', () => {
  const { result } = renderHook(() => useFilters(), { wrapper })
  act(() => {
    result.current.setFilters({
      exclude_users: 'bot',
      exclude_clusters: 'staging',
      exclude_names: 'web',
      exclude_operations: 'UPDATE',
    })
  })
  expect(result.current.filters.exclude_users).toBe('bot')
  expect(result.current.filters.exclude_clusters).toBe('staging')
  expect(result.current.filters.exclude_names).toBe('web')
  expect(result.current.filters.exclude_operations).toBe('UPDATE')
  act(() => {
    result.current.clearFilters()
  })
  expect(result.current.filters).toEqual({})
})
