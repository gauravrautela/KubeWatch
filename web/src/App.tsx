import { Routes, Route, useNavigate, useParams, useLocation } from 'react-router-dom'
import { DashboardPage } from './components/DashboardPage'
import { Drawer } from './components/Drawer'
import { EventDetail } from './components/EventDetail'

function EventDrawer() {
  const { id } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  if (!id) return null
  return (
    <Drawer onClose={() => navigate(`/${location.search}`)}>
      <EventDetail id={id} />
    </Drawer>
  )
}

export function App() {
  return (
    <>
      <DashboardPage />
      <Routes>
        <Route path="/events/:id" element={<EventDrawer />} />
        <Route path="*" element={null} />
      </Routes>
    </>
  )
}
