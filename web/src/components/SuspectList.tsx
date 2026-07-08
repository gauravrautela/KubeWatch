import { Fragment, useState } from 'react'
import { Link } from 'react-router-dom'
import type { Suspect } from '../types'

function scoreClasses(score: number): string {
  if (score >= 70) return 'border-red-500/30 bg-red-500/15 text-red-400'
  if (score >= 40) return 'border-amber-500/30 bg-amber-500/15 text-amber-400'
  return 'border-zinc-500/30 bg-zinc-500/15 text-zinc-400'
}

export function SuspectList({ suspects }: { suspects: Suspect[] }) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const keyOf = (s: Suspect) => `${s.namespace}/${s.kind}/${s.name}`
  const toggle = (k: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      return next
    })

  return (
    <table className="mt-6 w-full text-left text-sm">
      <thead>
        <tr className="border-b border-zinc-800 text-xs uppercase tracking-wide text-zinc-500">
          <th className="py-2 pr-3">Score</th>
          <th className="py-2 pr-3">Resource</th>
          <th className="py-2 pr-3">Why</th>
          <th className="py-2 pr-3">Changes</th>
          <th className="py-2">Latest</th>
        </tr>
      </thead>
      <tbody>
        {suspects.map((s) => {
          const k = keyOf(s)
          const open = expanded.has(k)
          return (
            <Fragment key={k}>
              <tr className="border-b border-zinc-900">
                <td className="py-2 pr-3">
                  <span className={`inline-block rounded border px-2 py-0.5 text-xs font-semibold ${scoreClasses(s.score)}`}>
                    {s.score}
                  </span>
                </td>
                <td className="py-2 pr-3">
                  <span className="text-zinc-500">{s.namespace}</span>{' '}
                  <span className="text-zinc-400">{s.kind}</span>{' '}
                  <span className="font-medium text-zinc-100">{s.name}</span>
                </td>
                <td className="py-2 pr-3">
                  <span className="flex flex-wrap gap-1">
                    {s.reasons.map((r) => (
                      <span key={r} className="rounded-full border border-zinc-700 px-2 py-0.5 text-xs text-zinc-300">
                        {r}
                      </span>
                    ))}
                  </span>
                </td>
                <td className="py-2 pr-3">
                  <button
                    onClick={() => toggle(k)}
                    className="text-xs text-zinc-400 underline decoration-dotted hover:text-zinc-200"
                  >
                    {s.event_count} change{s.event_count === 1 ? '' : 's'} {open ? '▾' : '▸'}
                  </button>
                </td>
                <td className="py-2 text-xs text-zinc-400">
                  {new Date(s.latest_event_time).toLocaleTimeString()}
                </td>
              </tr>
              {open ? (
                <tr className="border-b border-zinc-900 bg-zinc-900/40">
                  <td colSpan={5} className="px-4 py-2">
                    <ul className="space-y-1 text-xs">
                      {s.events.map((e) => (
                        <li key={e.event_id}>
                          <Link to={`/events/${e.event_id}`} className="text-emerald-400 hover:underline">
                            {new Date(e.event_time).toLocaleTimeString()} {e.operation}
                          </Link>{' '}
                          <span className="text-zinc-400">{e.classes.join(', ')}</span>{' '}
                          <span className="text-zinc-500">by {e.user_name}</span>
                        </li>
                      ))}
                    </ul>
                  </td>
                </tr>
              ) : null}
            </Fragment>
          )
        })}
      </tbody>
    </table>
  )
}
