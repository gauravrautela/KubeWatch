import { useEventsFeed } from '../api/hooks'
import { parseDiff, formatValue } from '../diff'
import type { Filters } from '../types'

// ResourceTimeline shows one object's change history as a chronological track.
export function ResourceTimeline({ filters }: { filters: Filters }) {
  const feed = useEventsFeed(filters)

  if (feed.isLoading) return <div>Loading timeline…</div>
  if (feed.isError) return <div>Failed to load timeline.</div>

  const rows = feed.data?.pages.flatMap((p) => p.events) ?? []
  if (rows.length === 0) return <div>No history for this resource.</div>

  return (
    <ol className="resource-timeline">
      {rows.map((r) => {
        const changes = parseDiff(r.diff)
        return (
          <li key={r.event_id} className="timeline-entry">
            <div className="timeline-when">{new Date(r.event_time).toLocaleString()} — {r.operation} by {r.user_name}</div>
            <ul>
              {changes.map((c) => (
                <li key={c.path}>
                  <code>{c.path}</code>: {formatValue(c.old)} → {formatValue(c.new)}
                </li>
              ))}
            </ul>
          </li>
        )
      })}
    </ol>
  )
}
