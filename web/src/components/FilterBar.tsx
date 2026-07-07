import { useEffect, useState } from 'react'
import { useFacets } from '../api/hooks'
import { useFilters, addToList } from '../useFilters'
import { presetFrom, PRESETS, type Preset } from '../timeRange'

const fieldCls =
  'rounded-md border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-sm text-zinc-200 ' +
  'placeholder:text-zinc-500 focus:border-sky-600 focus:outline-none'

export function FilterBar() {
  const { filters, setFilter, setFilters } = useFilters()
  const facets = useFacets()

  const clusters = facets.data?.clusters ?? []
  const namespaces = facets.data?.namespaces ?? []
  const kinds = facets.data?.kinds ?? []
  const operations = facets.data?.operations ?? []

  // Search box: local value debounced 300ms into the `q` URL param.
  const [search, setSearch] = useState(filters.q ?? '')
  useEffect(() => {
    setSearch(filters.q ?? '')
  }, [filters.q])
  useEffect(() => {
    const t = setTimeout(() => {
      if (search !== (filters.q ?? '')) setFilter('q', search)
    }, 300)
    return () => clearTimeout(t)
  }, [search, filters.q, setFilter])

  // Time range: preset select; presets set only `from` (live window),
  // custom applies both bounds. Values are always RFC3339 (the API 400s otherwise).
  const [range, setRange] = useState(filters.from || filters.to ? 'custom' : '')
  const [fromLocal, setFromLocal] = useState('')
  const [toLocal, setToLocal] = useState('')
  const onRange = (v: string) => {
    setRange(v)
    if (v === '') setFilters({ from: '', to: '' })
    else if (v !== 'custom') setFilters({ from: presetFrom(v as Preset), to: '' })
  }
  const applyCustom = () => {
    const f = new Date(fromLocal)
    const t = new Date(toLocal)
    if (isNaN(f.getTime()) || isNaN(t.getTime())) return
    setFilters({ from: f.toISOString(), to: t.toISOString() })
  }

  const onIgnore = (v: string) => {
    if (!v) return
    const sep = v.indexOf(':')
    const dim = v.slice(0, sep)
    const val = v.slice(sep + 1)
    if (dim === 'kind') setFilter('exclude_kinds', addToList(filters.exclude_kinds, val))
    else setFilter('exclude_namespaces', addToList(filters.exclude_namespaces, val))
  }

  const select = (
    label: string,
    key: 'cluster' | 'namespace' | 'kind' | 'operation',
    values: string[],
  ) => (
    <select
      aria-label={label}
      className={fieldCls}
      value={filters[key] ?? ''}
      onChange={(e) => setFilter(key, e.target.value)}
    >
      <option value="">{label}: all</option>
      {values.map((v) => (
        <option key={v} value={v}>
          {v}
        </option>
      ))}
    </select>
  )

  return (
    <div className="flex flex-wrap items-center gap-2">
      <input
        aria-label="search"
        className={`${fieldCls} w-56`}
        placeholder="Search name or user…"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
      />
      {select('cluster', 'cluster', clusters)}
      {select('namespace', 'namespace', namespaces)}
      {select('kind', 'kind', kinds)}
      {select('operation', 'operation', operations)}

      <select aria-label="time range" className={fieldCls} value={range} onChange={(e) => onRange(e.target.value)}>
        <option value="">Time: all</option>
        {(Object.keys(PRESETS) as Preset[]).map((p) => (
          <option key={p} value={p}>
            Last {p}
          </option>
        ))}
        <option value="custom">Custom…</option>
      </select>
      {range === 'custom' && (
        <span className="flex items-center gap-1">
          <input aria-label="from" type="datetime-local" className={fieldCls} value={fromLocal} onChange={(e) => setFromLocal(e.target.value)} />
          <span className="text-zinc-500">–</span>
          <input aria-label="to" type="datetime-local" className={fieldCls} value={toLocal} onChange={(e) => setToLocal(e.target.value)} />
          <button className="rounded-md bg-sky-700 px-2 py-1.5 text-sm text-white hover:bg-sky-600" onClick={applyCustom}>
            Apply
          </button>
        </span>
      )}

      <select aria-label="ignore" className={fieldCls} value="" onChange={(e) => onIgnore(e.target.value)}>
        <option value="">Ignore…</option>
        <optgroup label="Kinds">
          {kinds.map((k) => (
            <option key={k} value={`kind:${k}`}>
              {`kind: ${k}`}
            </option>
          ))}
        </optgroup>
        <optgroup label="Namespaces">
          {namespaces.map((n) => (
            <option key={n} value={`ns:${n}`}>
              {`ns: ${n}`}
            </option>
          ))}
        </optgroup>
      </select>
    </div>
  )
}
