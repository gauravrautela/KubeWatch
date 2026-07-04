import { useEventsFeed } from '../api/hooks'
import { useLiveFeed } from '../useLiveFeed'
import type { Filters, Row } from '../types'

function summarizeDiff(diff: string): string {
  try {
    const changes = JSON.parse(diff) as unknown[]
    return changes.length === 1 ? '1 change' : `${changes.length} changes`
  } catch {
    return ''
  }
}

export function EventList({ filters, onSelect }: { filters: Filters; onSelect: (id: string) => void }) {
  const feed = useEventsFeed(filters)
  useLiveFeed(filters)

  if (feed.isLoading) return <div className="feed-loading">Loading changes…</div>
  if (feed.isError) {
    return (
      <div className="feed-error">
        Failed to load changes. <button onClick={() => feed.refetch()}>Retry</button>
      </div>
    )
  }

  const rows: Row[] = feed.data?.pages.flatMap((p) => p.events) ?? []
  if (rows.length === 0) return <div className="feed-empty">No changes match these filters.</div>

  return (
    <div className="event-list">
      <table>
        <tbody>
          {rows.map((r) => (
            <tr
              key={r.event_id}
              onClick={() => onSelect(r.event_id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  onSelect(r.event_id)
                }
              }}
              tabIndex={0}
              role="button"
              className="event-row"
            >
              <td>{new Date(r.event_time).toLocaleString()}</td>
              <td>{r.operation}</td>
              <td>{r.cluster}</td>
              <td>{r.namespace}/{r.kind}/{r.name}</td>
              <td>{r.user_name}</td>
              <td>{summarizeDiff(r.diff)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {feed.hasNextPage && (
        <button onClick={() => feed.fetchNextPage()} disabled={feed.isFetchingNextPage}>
          {feed.isFetchingNextPage ? 'Loading…' : 'Load older'}
        </button>
      )}
    </div>
  )
}
