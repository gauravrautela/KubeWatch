import type { MouseEvent, ReactNode } from 'react'
import { useFilters, addToList, removeFromList } from '../useFilters'
import type { Filters } from '../types'

export type FilterField = 'kind' | 'namespace' | 'name' | 'user' | 'cluster' | 'operation'

const EXCLUDE_KEY: Record<FilterField, keyof Filters> = {
  kind: 'exclude_kinds',
  namespace: 'exclude_namespaces',
  name: 'exclude_names',
  user: 'exclude_users',
  cluster: 'exclude_clusters',
  operation: 'exclude_operations',
}

const btnCls = 'cursor-pointer rounded px-1 text-lg leading-none text-zinc-500 hover:text-zinc-100'

// Wraps a table-cell value with hover-revealed one-click include (⊕) and
// exclude (⊖) filter buttons. Include sets the single-value param; exclude
// appends to the field's CSV exclude param. Applying one side always clears
// the same value from the other so the two can never contradict.
export function CellFilter({ field, value, children }: { field: FilterField; value: string; children: ReactNode }) {
  const { filters, setFilters } = useFilters()
  if (!value) return <>{children}</>
  const excludeKey = EXCLUDE_KEY[field]

  const include = (e: MouseEvent) => {
    e.stopPropagation()
    setFilters({ [field]: value, [excludeKey]: removeFromList(filters[excludeKey], value) })
  }
  const exclude = (e: MouseEvent) => {
    e.stopPropagation()
    setFilters({
      [excludeKey]: addToList(filters[excludeKey], value),
      ...(filters[field] === value ? { [field]: '' } : {}),
    })
  }

  return (
    <span className="group/cf inline-flex items-center gap-1">
      {children}
      <span
        className="hidden items-center group-hover/cf:inline-flex"
        onKeyDown={(e) => e.stopPropagation()}
      >
        <button aria-label={`include ${field} ${value}`} title="Include" className={btnCls} onClick={include}>
          ⊕
        </button>
        <button aria-label={`exclude ${field} ${value}`} title="Exclude" className={btnCls} onClick={exclude}>
          ⊖
        </button>
      </span>
    </span>
  )
}
