import { useState } from 'react'
import { useEvent } from '../api/hooks'
import { parseDiff, formatValue } from '../diff'

function pretty(json: string): string {
  try {
    return JSON.stringify(JSON.parse(json), null, 2)
  } catch {
    return json
  }
}

export function EventDetail({ id }: { id: string }) {
  const [raw, setRaw] = useState(false)
  const { data, isLoading, isError, refetch } = useEvent(id)

  if (isLoading) return <div className="detail-loading">Loading…</div>
  if (isError || !data) {
    return (
      <div className="detail-error">
        Failed to load event. <button onClick={() => refetch()}>Retry</button>
      </div>
    )
  }

  const changes = parseDiff(data.diff)

  return (
    <div className="event-detail">
      <header className="detail-meta">
        <div><strong>{data.operation}</strong> {data.namespace}/{data.kind}/{data.name}</div>
        <div>by <strong>{data.user_name}</strong> on {data.cluster}</div>
        <div>{new Date(data.event_time).toLocaleString()}</div>
        {data.dry_run && <span className="badge">dry-run</span>}
      </header>

      <button onClick={() => setRaw((v) => !v)}>{raw ? 'View structured' : 'View raw'}</button>

      {raw ? (
        <div className="raw-diff">
          <pre aria-label="before">{pretty(data.old_object)}</pre>
          <pre aria-label="after">{pretty(data.new_object)}</pre>
        </div>
      ) : (
        <ul className="structured-diff">
          {changes.length === 0 && <li>No field-level changes.</li>}
          {changes.map((c) => (
            <li key={c.path} className={`diff-${c.op}`}>
              <code>{c.path}</code> <span className="op">{c.op}</span>{' '}
              {formatValue(c.old)} → {formatValue(c.new)}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
