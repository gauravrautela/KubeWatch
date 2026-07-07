const STYLES: Record<string, string> = {
  CREATE: 'bg-emerald-950 text-emerald-300 ring-emerald-800',
  UPDATE: 'bg-amber-950 text-amber-300 ring-amber-800',
  DELETE: 'bg-red-950 text-red-300 ring-red-800',
}

export function OperationBadge({ op }: { op: string }) {
  const cls = STYLES[op] ?? 'bg-zinc-800 text-zinc-300 ring-zinc-700'
  return (
    <span className={`inline-block rounded px-1.5 py-0.5 text-xs font-semibold ring-1 ${cls}`}>
      {op}
    </span>
  )
}
