import { Routes, Route } from 'react-router-dom'
import { DashboardPage } from './components/DashboardPage'
import { EventDetailPage } from './components/EventDetail'
import { Header } from './components/Header'
import { IncidentPage } from './components/IncidentPage'

export function App() {
  return (
    <div className="min-h-screen">
      <Header />
      <Routes>
        <Route path="/events/:id" element={<EventDetailPage />} />
        <Route path="/incident" element={<IncidentPage />} />
        <Route path="*" element={<DashboardPage />} />
      </Routes>
    </div>
  )
}
