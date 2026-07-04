import { test, expect } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { useFilters } from './useFilters'

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
