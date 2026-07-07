import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { useActivity } from '../api/hooks'
import { useFilters } from '../useFilters'
import { pickBucket, bucketRange } from '../timeRange'

export function ActivityHistogram() {
  const { filters, setFilters } = useFilters()
  const bucket = pickBucket(filters.from, filters.to)
  const activity = useActivity(filters, bucket)

  if (activity.isLoading) return <div className="py-8 text-center text-sm text-zinc-500">Loading activity…</div>
  if (activity.isError) return <div className="py-8 text-center text-sm text-zinc-400">Failed to load activity.</div>

  const data = (activity.data ?? []).map((b) => ({
    iso: b.bucket_start,
    label: new Date(b.bucket_start).toLocaleString(),
    count: b.count,
  }))
  if (data.length === 0) return <div className="py-8 text-center text-sm text-zinc-500">No activity in this window.</div>

  // Bar click zooms the time range to that bucket.
  const onBarClick = (entry: unknown) => {
    const e = entry as { iso?: string; payload?: { iso?: string } }
    const iso = e?.iso ?? e?.payload?.iso
    if (iso) setFilters(bucketRange(iso, bucket))
  }

  return (
    <div aria-label="activity histogram" className="h-40 w-full">
      <ResponsiveContainer>
        <BarChart data={data}>
          <XAxis dataKey="label" hide />
          <YAxis allowDecimals={false} width={30} stroke="#52525b" tick={{ fill: '#a1a1aa', fontSize: 11 }} />
          <Tooltip
            cursor={{ fill: 'rgba(255,255,255,0.05)' }}
            contentStyle={{ background: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, color: '#e4e4e7' }}
          />
          <Bar dataKey="count" fill="#0284c7" radius={[2, 2, 0, 0]} onClick={onBarClick} cursor="pointer" />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
