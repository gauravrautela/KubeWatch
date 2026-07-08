import { useState, type Dispatch, type SetStateAction } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchIncident, type IncidentQuery } from '../api/client'
import { useFacets } from '../api/hooks'
import { SuspectList } from './SuspectList'

const LOOKBACKS = ['15m', '1h', '6h', '24h']
const OPERATIONS = ['CREATE', 'UPDATE', 'DELETE']

const inputClasses =
  'rounded border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-sm text-zinc-100'

export function IncidentPage() {
  const { data: facets, error: facetsError } = useFacets()
  const [cluster, setCluster] = useState('')
  const [at, setAt] = useState('') // datetime-local; '' means "now"
  const [lookback, setLookback] = useState('1h')
  const [query, setQuery] = useState<IncidentQuery | null>(null)
  // Server-side filters; empty set means "all". Toggling refetches.
  const [kindFilter, setKindFilter] = useState<Set<string>>(new Set())
  const [opFilter, setOpFilter] = useState<Set<string>>(new Set())

  // The effective query sent to the API: the analyzed window plus any
  // active kind/operation filters, so filtering re-ranks server-side.
  const effective: IncidentQuery | null = query
    ? {
        ...query,
        ...(kindFilter.size ? { kinds: [...kindFilter].sort().join(',') } : {}),
        ...(opFilter.size ? { operations: [...opFilter].sort().join(',') } : {}),
      }
    : null

  const { data, isFetching, error } = useQuery({
    queryKey: ['incident', effective],
    queryFn: () => fetchIncident(effective!),
    enabled: effective !== null,
  })

  const analyze = () => {
    if (!cluster) return
    setKindFilter(new Set())
    setOpFilter(new Set())
    setQuery({
      cluster,
      lookback,
      at: at ? new Date(at).toISOString() : new Date().toISOString(),
    })
  }

  const toggleIn = (set: Dispatch<SetStateAction<Set<string>>>) => (v: string) =>
    set((prev) => {
      const next = new Set(prev)
      if (next.has(v)) next.delete(v)
      else next.add(v)
      return next
    })
  const toggleKind = toggleIn(setKindFilter)
  const toggleOp = toggleIn(setOpFilter)

  const suspects = data?.suspects ?? []
  const filtersActive = kindFilter.size > 0 || opFilter.size > 0

  const chip = (label: string, text: string, on: boolean, onClick: () => void) => (
    <button
      key={text}
      aria-label={label}
      aria-pressed={on}
      onClick={onClick}
      className={`rounded-full border px-2 py-0.5 text-xs ${
        on
          ? 'border-emerald-500/50 bg-emerald-500/15 text-emerald-300'
          : 'border-zinc-700 text-zinc-400 hover:text-zinc-200'
      }`}
    >
      {text}
    </button>
  )

  return (
    <main className="mx-auto max-w-6xl px-4 py-6">
      <h1 className="text-lg font-semibold text-zinc-100">Incident Lens</h1>
      <p className="mt-1 text-sm text-zinc-400">
        Ranked, noise-filtered view of what changed in a cluster around an incident.
      </p>
      <div className="mt-4 flex flex-wrap items-end gap-3">
        {facetsError ? (
          <p className="text-sm text-red-400">
            failed to load clusters: {(facetsError as Error).message}
          </p>
        ) : facets ? (
          <label className="flex flex-col gap-1 text-xs text-zinc-400">
            Cluster
            <select value={cluster} onChange={(e) => setCluster(e.target.value)} className={inputClasses}>
              <option value="">select cluster…</option>
              {facets.clusters.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </label>
        ) : (
          <p className="text-xs text-zinc-500">Loading clusters…</p>
        )}
        <label className="flex flex-col gap-1 text-xs text-zinc-400">
          Incident time (blank = now)
          <input type="datetime-local" value={at} onChange={(e) => setAt(e.target.value)} className={inputClasses} />
        </label>
        <label className="flex flex-col gap-1 text-xs text-zinc-400">
          Lookback
          <select value={lookback} onChange={(e) => setLookback(e.target.value)} className={inputClasses}>
            {LOOKBACKS.map((l) => (
              <option key={l} value={l}>
                {l}
              </option>
            ))}
          </select>
        </label>
        <button
          onClick={analyze}
          disabled={!cluster || isFetching}
          className="rounded bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-emerald-500 disabled:opacity-50"
        >
          {isFetching ? 'Analyzing…' : 'Analyze'}
        </button>
      </div>
      {query ? (
        <div className="mt-6 flex flex-wrap items-center gap-x-5 gap-y-2">
          <span className="flex flex-wrap items-center gap-1.5" aria-label="filter by kind">
            <span className="text-xs text-zinc-500">Kinds:</span>
            {(facets?.kinds ?? []).map((k) => chip(`kind ${k}`, k, kindFilter.has(k), () => toggleKind(k)))}
          </span>
          <span className="flex flex-wrap items-center gap-1.5" aria-label="filter by operation">
            <span className="text-xs text-zinc-500">Operations:</span>
            {OPERATIONS.map((o) => chip(`operation ${o}`, o, opFilter.has(o), () => toggleOp(o)))}
          </span>
        </div>
      ) : null}
      {error ? <p className="mt-6 text-sm text-red-400">{(error as Error).message}</p> : null}
      {data && suspects.length === 0 ? (
        <p className="mt-6 text-sm text-zinc-400">
          {filtersActive
            ? 'No changes match the current filters in this window.'
            : 'No changes found in this window. Try widening the lookback.'}
        </p>
      ) : null}
      {suspects.length > 0 ? <SuspectList suspects={suspects} /> : null}
    </main>
  )
}
