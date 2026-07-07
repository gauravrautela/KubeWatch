import { Routes, Route } from 'react-router-dom'
import { DashboardPage } from './components/DashboardPage'
import { EventDetailPage } from './components/EventDetail'
import { Header } from './components/Header'

export function App() {
  return (
    <div className="min-h-screen">
      <Header />
      <Routes>
        <Route path="/events/:id" element={<EventDetailPage />} />
        <Route path="*" element={<DashboardPage />} />
      </Routes>
    </div>
  )
}
