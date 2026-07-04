import { Bar, BarChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { useActivity } from '../api/hooks'
import { useFilters } from '../useFilters'

const BUCKET = 'hour'

export function ActivityHistogram() {
  const { filters } = useFilters()
  const activity = useActivity(filters, BUCKET)

  if (activity.isLoading) return <div className="hist-loading">Loading activity…</div>
  if (activity.isError) return <div className="hist-error">Failed to load activity.</div>

  const data = (activity.data ?? []).map((b) => ({
    label: new Date(b.bucket_start).toLocaleString(),
    count: b.count,
  }))
  if (data.length === 0) return <div className="hist-empty">No activity in this window.</div>

  return (
    <div aria-label="activity histogram" className="activity-histogram" style={{ width: '100%', height: 160 }}>
      <ResponsiveContainer>
        <BarChart data={data}>
          <XAxis dataKey="label" hide />
          <YAxis allowDecimals={false} width={30} />
          <Tooltip />
          <Bar dataKey="count" fill="#4f46e5" />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
