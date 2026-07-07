import { useLocation, useNavigate } from 'react-router-dom'
import { useFilters } from '../useFilters'
import { FilterBar } from './FilterBar'
import { ActivityHistogram } from './ActivityHistogram'
import { EventList } from './EventList'

export function DashboardPage() {
  const { filters } = useFilters()
  const navigate = useNavigate()
  const location = useLocation()

  return (
    <div className="dashboard">
      <FilterBar />
      <ActivityHistogram />
      <EventList filters={filters} onSelect={(id) => navigate(`/events/${id}${location.search}`)} />
    </div>
  )
}
