import { useSearchParams } from 'react-router-dom'
import type { Filters } from './types'

const FILTER_KEYS: (keyof Filters)[] = [
  'cluster', 'namespace', 'kind', 'name', 'user', 'operation', 'from', 'to',
]

export function useFilters() {
  const [params, setParams] = useSearchParams()

  const filters: Filters = {}
  for (const key of FILTER_KEYS) {
    const v = params.get(key)
    if (v) filters[key] = v
  }

  function setFilter(key: keyof Filters, value: string) {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (value) next.set(key, value)
        else next.delete(key)
        return next
      },
      { replace: true },
    )
  }

  return { filters, setFilter }
}
