import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchIncident, type IncidentQuery } from '../api/client'
import { useFacets } from '../api/hooks'
import { SuspectList } from './SuspectList'

const LOOKBACKS = ['15m', '1h', '6h', '24h']

const inputClasses =
  'rounded border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-sm text-zinc-100'

export function IncidentPage() {
  const { data: facets, error: facetsError } = useFacets()
  const [cluster, setCluster] = useState('')
  const [at, setAt] = useState('') // datetime-local; '' means "now"
  const [lookback, setLookback] = useState('1h')
  const [query, setQuery] = useState<IncidentQuery | null>(null)
  // Kinds toggled on; empty set means "show all kinds".
  const [kindFilter, setKindFilter] = useState<Set<string>>(new Set())

  const { data, isFetching, error } = useQuery({
    queryKey: ['incident', query],
    queryFn: () => fetchIncident(query!),
    enabled: query !== null,
  })

  const analyze = () => {
    if (!cluster) return
    setKindFilter(new Set())
    setQuery({
      cluster,
      lookback,
      at: at ? new Date(at).toISOString() : new Date().toISOString(),
    })
  }

  const toggleKind = (kind: string) =>
    setKindFilter((prev) => {
      const next = new Set(prev)
      if (next.has(kind)) next.delete(kind)
      else next.add(kind)
      return next
    })

  const suspects = data?.suspects ?? []
  const kinds = [...new Set(suspects.map((s) => s.kind))].sort()
  const visibleSuspects = kindFilter.size ? suspects.filter((s) => kindFilter.has(s.kind)) : suspects

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
      {error ? <p className="mt-6 text-sm text-red-400">{(error as Error).message}</p> : null}
      {data && suspects.length === 0 ? (
        <p className="mt-6 text-sm text-zinc-400">
          No changes found in this window. Try widening the lookback.
        </p>
      ) : null}
      {suspects.length > 0 ? (
        <div className="mt-6 flex flex-wrap items-center gap-1.5" aria-label="filter by kind">
          <span className="mr-1 text-xs text-zinc-500">Kinds:</span>
          {kinds.map((k) => {
            const on = kindFilter.has(k)
            return (
              <button
                key={k}
                aria-label={`kind ${k}`}
                aria-pressed={on}
                onClick={() => toggleKind(k)}
                className={`rounded-full border px-2 py-0.5 text-xs ${
                  on
                    ? 'border-emerald-500/50 bg-emerald-500/15 text-emerald-300'
                    : 'border-zinc-700 text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {k}
              </button>
            )
          })}
        </div>
      ) : null}
      {visibleSuspects.length > 0 ? <SuspectList suspects={visibleSuspects} /> : null}
    </main>
  )
}
