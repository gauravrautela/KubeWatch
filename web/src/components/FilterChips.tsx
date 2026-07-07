import { useFilters, splitList, removeFromList } from '../useFilters'
import type { Filters } from '../types'

const SIMPLE_KEYS: (keyof Filters)[] = [
  'q', 'cluster', 'namespace', 'kind', 'name', 'user', 'operation', 'from', 'to',
]

function chipLabel(key: string, value: string): string {
  if (key === 'from' || key === 'to') return `${key}: ${new Date(value).toLocaleString()}`
  return `${key}: ${value}`
}

export function FilterChips() {
  const { filters, setFilter, clearFilters } = useFilters()

  const chips: { label: string; onRemove: () => void }[] = []
  for (const key of SIMPLE_KEYS) {
    const v = filters[key]
    if (v) chips.push({ label: chipLabel(key, v), onRemove: () => setFilter(key, '') })
  }
  for (const v of splitList(filters.exclude_kinds)) {
    chips.push({
      label: `not kind: ${v}`,
      onRemove: () => setFilter('exclude_kinds', removeFromList(filters.exclude_kinds, v)),
    })
  }
  for (const v of splitList(filters.exclude_namespaces)) {
    chips.push({
      label: `not ns: ${v}`,
      onRemove: () => setFilter('exclude_namespaces', removeFromList(filters.exclude_namespaces, v)),
    })
  }

  if (chips.length === 0) return null

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {chips.map((c) => (
        <span
          key={c.label}
          className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs ${
            c.label.startsWith('not ')
              ? 'border border-red-900 bg-red-950/50 text-red-300'
              : 'border border-zinc-700 bg-zinc-800 text-zinc-300'
          }`}
        >
          {c.label}
          <button
            aria-label={`remove ${c.label}`}
            className="text-zinc-500 hover:text-zinc-200"
            onClick={c.onRemove}
          >
            ×
          </button>
        </span>
      ))}
      <button
        className="ml-1 text-xs text-zinc-500 underline hover:text-zinc-300"
        onClick={clearFilters}
      >
        Clear all
      </button>
    </div>
  )
}
