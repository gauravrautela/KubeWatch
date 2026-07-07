import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { Filters } from './types'

const FILTER_KEYS: (keyof Filters)[] = [
  'q', 'cluster', 'namespace', 'kind', 'name', 'user', 'operation',
  'from', 'to', 'exclude_kinds', 'exclude_namespaces',
  'exclude_users', 'exclude_clusters', 'exclude_names', 'exclude_operations',
]

// csv helpers for the multi-value exclude params.
export function splitList(csv?: string): string[] {
  return csv ? csv.split(',').filter(Boolean) : []
}

export function addToList(csv: string | undefined, value: string): string {
  const items = splitList(csv)
  if (!items.includes(value)) items.push(value)
  return items.join(',')
}

export function removeFromList(csv: string | undefined, value: string): string {
  return splitList(csv).filter((v) => v !== value).join(',')
}

export function useFilters() {
  const [params, setParams] = useSearchParams()

  // A change-detection key over just the filter params, so the memoized filters
  // object keeps a stable identity across re-renders when they are unchanged.
  const filterKey = FILTER_KEYS.map((k) => params.get(k) ?? '').join('\n')

  const filters = useMemo<Filters>(() => {
    const f: Filters = {}
    for (const key of FILTER_KEYS) {
      const v = params.get(key)
      if (v) f[key] = v
    }
    return f
    // filterKey fully captures the params we read; params identity is not stable.
  }, [filterKey]) // eslint-disable-line react-hooks/exhaustive-deps

  const setFilters = useCallback(
    (patch: Partial<Record<keyof Filters, string>>) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          for (const [key, value] of Object.entries(patch)) {
            if (value) next.set(key, value)
            else next.delete(key)
          }
          return next
        },
        { replace: true },
      )
    },
    [setParams],
  )

  const setFilter = useCallback(
    (key: keyof Filters, value: string) => setFilters({ [key]: value }),
    [setFilters],
  )

  const clearFilters = useCallback(() => {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        for (const key of FILTER_KEYS) next.delete(key)
        return next
      },
      { replace: true },
    )
  }, [setParams])

  return { filters, setFilter, setFilters, clearFilters }
}
