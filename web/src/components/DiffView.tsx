import { useMemo, useState } from 'react'
import { buildUnifiedDiff } from '../unifiedDiff'

const LINE_CLS = {
  add: 'bg-emerald-950/50 text-emerald-200',
  del: 'bg-red-950/50 text-red-300',
  ctx: 'text-zinc-400',
} as const

const MARKER = { add: '+', del: '-', ctx: ' ' } as const

export function DiffView({ oldJson, newJson }: { oldJson: string; newJson: string }) {
  const segments = useMemo(() => buildUnifiedDiff(oldJson, newJson), [oldJson, newJson])
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set())
  const expand = (i: number) => setExpanded((prev) => new Set(prev).add(i))

  if (segments.length === 0) {
    return <div className="py-6 text-center text-sm text-zinc-500">No content to diff.</div>
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-zinc-800 bg-zinc-950 py-1 font-mono text-xs leading-5">
      {segments.map((s, i) =>
        s.kind === 'fold' && !expanded.has(i) ? (
          <button
            key={i}
            onClick={() => expand(i)}
            className="block w-full bg-zinc-900/80 px-3 py-1 text-center text-zinc-500 hover:text-zinc-300"
          >
            ⋯ {s.lines.length} unchanged lines
          </button>
        ) : (
          <div key={i}>
            {s.lines.map((l, j) => (
              <div key={j} data-line className={`whitespace-pre px-3 ${LINE_CLS[l.type]}`}>
                {MARKER[l.type]} {l.text}
              </div>
            ))}
          </div>
        ),
      )}
    </div>
  )
}
