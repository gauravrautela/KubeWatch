import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { Filters } from './types'

const FILTER_KEYS: (keyof Filters)[] = [
  'cluster', 'namespace', 'kind', 'name', 'user', 'operation', 'from', 'to',
]

export function useFilters() {
  const [params, setParams] = useSearchParams()

  // A change-detection key over just the filter params, so the memoized filters
  // object keeps a stable identity across re-renders when they are unchanged.
  const filterKey = FILTER_KEYS.map((k) => params.get(k) ?? '').join('')

  const filters = useMemo<Filters>(() => {
    const f: Filters = {}
    for (const key of FILTER_KEYS) {
      const v = params.get(key)
      if (v) f[key] = v
    }
    return f
    // filterKey fully captures the params we read; params identity is not stable.
  }, [filterKey]) // eslint-disable-line react-hooks/exhaustive-deps

  const setFilter = useCallback(
    (key: keyof Filters, value: string) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (value) next.set(key, value)
          else next.delete(key)
          return next
        },
        { replace: true },
      )
    },
    [setParams],
  )

  return { filters, setFilter }
}
