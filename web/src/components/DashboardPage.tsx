import { useLocation, useNavigate } from 'react-router-dom'
import { useFilters } from '../useFilters'
import { FilterBar } from './FilterBar'
import { FilterChips } from './FilterChips'
import { ActivityHistogram } from './ActivityHistogram'
import { EventList } from './EventList'

export function DashboardPage() {
  const { filters } = useFilters()
  const navigate = useNavigate()
  const location = useLocation()

  return (
    <main className="mx-auto max-w-6xl space-y-4 px-4 py-6">
      <FilterBar />
      <FilterChips />
      <section className="rounded-lg border border-zinc-800 bg-zinc-900/60 p-4">
        <ActivityHistogram />
      </section>
      <section className="overflow-hidden rounded-lg border border-zinc-800 bg-zinc-900/60">
        <EventList filters={filters} onSelect={(id) => navigate(`/events/${id}${location.search}`)} />
      </section>
    </main>
  )
}
