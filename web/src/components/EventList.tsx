import { useEventsFeed } from '../api/hooks'
import { useLiveFeed } from '../useLiveFeed'
import { timeAgo } from '../time'
import { OperationBadge } from './OperationBadge'
import type { Filters, Row } from '../types'

function summarizeDiff(diff: string): string {
  try {
    const changes = JSON.parse(diff) as unknown[]
    if (!Array.isArray(changes) || changes.length === 0) return ''
    return changes.length === 1 ? '1 change' : `${changes.length} changes`
  } catch {
    return ''
  }
}

const thCls = 'px-3 py-2 text-left text-xs font-semibold uppercase tracking-wide text-zinc-500'

export function EventList({ filters, onSelect }: { filters: Filters; onSelect: (id: string) => void }) {
  const feed = useEventsFeed(filters)
  useLiveFeed(filters)

  if (feed.isLoading) return <div className="py-10 text-center text-sm text-zinc-500">Loading changes…</div>
  if (feed.isError) {
    return (
      <div className="py-10 text-center text-sm text-zinc-400">
        Failed to load changes.{' '}
        <button className="text-sky-400 underline" onClick={() => feed.refetch()}>
          Retry
        </button>
      </div>
    )
  }

  const rows: Row[] = feed.data?.pages.flatMap((p) => p.events) ?? []
  if (rows.length === 0) {
    return <div className="py-10 text-center text-sm text-zinc-500">No changes match these filters.</div>
  }

  return (
    <div>
      <table className="w-full text-sm">
        <thead className="border-b border-zinc-800">
          <tr>
            <th className={thCls}>Time</th>
            <th className={thCls}>Operation</th>
            <th className={thCls}>Resource</th>
            <th className={thCls}>User</th>
            <th className={thCls}>Cluster</th>
            <th className={thCls}>Changes</th>
          </tr>
        </thead>
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
              className="cursor-pointer border-b border-zinc-800/60 hover:bg-zinc-800/40 focus:bg-zinc-800/40 focus:outline-none"
            >
              <td className="whitespace-nowrap px-3 py-2 text-zinc-400" title={new Date(r.event_time).toISOString()}>
                {timeAgo(r.event_time)}
              </td>
              <td className="whitespace-nowrap px-3 py-2">
                <OperationBadge op={r.operation} />
                {r.dry_run && (
                  <span className="ml-1.5 rounded border border-zinc-600 px-1 py-0.5 text-xs text-zinc-400">dry-run</span>
                )}
              </td>
              <td className="px-3 py-2 font-mono text-xs">
                <span className="text-zinc-500">{r.kind}</span>{' '}
                <span className="text-zinc-100">{r.namespace ? `${r.namespace}/${r.name}` : r.name}</span>
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-zinc-300">{r.user_name}</td>
              <td className="whitespace-nowrap px-3 py-2 text-zinc-400">{r.cluster}</td>
              <td className="whitespace-nowrap px-3 py-2 text-zinc-500">{summarizeDiff(r.diff)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="flex items-center justify-between px-3 py-2 text-xs text-zinc-500">
        <span>{rows.length} events loaded</span>
        {feed.hasNextPage && (
          <button
            className="rounded border border-zinc-700 px-2 py-1 text-zinc-300 hover:bg-zinc-800"
            onClick={() => feed.fetchNextPage()}
            disabled={feed.isFetchingNextPage}
          >
            {feed.isFetchingNextPage ? 'Loading…' : 'Load older'}
          </button>
        )}
      </div>
    </div>
  )
}
