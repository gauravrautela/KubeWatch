import { useFacets } from '../api/hooks'
import { useFilters } from '../useFilters'

export function FilterBar() {
  const { filters, setFilter } = useFilters()
  const facets = useFacets()

  const clusters = facets.data?.clusters ?? []
  const kinds = facets.data?.kinds ?? []
  const operations = facets.data?.operations ?? []

  return (
    <div className="filter-bar">
      <label>
        cluster
        <select
          aria-label="cluster"
          value={filters.cluster ?? ''}
          onChange={(e) => setFilter('cluster', e.target.value)}
        >
          <option value="">all</option>
          {clusters.map((c) => (
            <option key={c} value={c}>{c}</option>
          ))}
        </select>
      </label>

      <label>
        kind
        <select
          aria-label="kind"
          value={filters.kind ?? ''}
          onChange={(e) => setFilter('kind', e.target.value)}
        >
          <option value="">all</option>
          {kinds.map((k) => (
            <option key={k} value={k}>{k}</option>
          ))}
        </select>
      </label>

      <label>
        operation
        <select
          aria-label="operation"
          value={filters.operation ?? ''}
          onChange={(e) => setFilter('operation', e.target.value)}
        >
          <option value="">all</option>
          {operations.map((o) => (
            <option key={o} value={o}>{o}</option>
          ))}
        </select>
      </label>

      <label>
        name
        <input
          aria-label="name"
          value={filters.name ?? ''}
          onChange={(e) => setFilter('name', e.target.value)}
        />
      </label>

      <label>
        user
        <input
          aria-label="user"
          value={filters.user ?? ''}
          onChange={(e) => setFilter('user', e.target.value)}
        />
      </label>
    </div>
  )
}
