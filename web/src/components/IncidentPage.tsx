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

  const { data, isFetching, error } = useQuery({
    queryKey: ['incident', query],
    queryFn: () => fetchIncident(query!),
    enabled: query !== null,
  })

  const analyze = () => {
    if (!cluster) return
    setQuery({
      cluster,
      lookback,
      at: at ? new Date(at).toISOString() : new Date().toISOString(),
    })
  }

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
      {data && data.suspects.length === 0 ? (
        <p className="mt-6 text-sm text-zinc-400">
          No changes found in this window. Try widening the lookback.
        </p>
      ) : null}
      {data && data.suspects.length > 0 ? <SuspectList suspects={data.suspects} /> : null}
    </main>
  )
}
