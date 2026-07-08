import { test, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { SuspectList } from './SuspectList'
import type { Suspect } from '../types'

const suspect: Suspect = {
  namespace: 'payments', kind: 'Deployment', name: 'checkout',
  score: 87, reasons: ['image change', 'human: alice', 'first change in 12d'],
  event_count: 2, latest_event_time: '2026-07-08T13:58:00Z',
  events: [
    { event_id: 'ev-1', event_time: '2026-07-08T13:58:00Z', operation: 'UPDATE',
      classes: ['image'], actor_type: 'human', user_name: 'alice' },
    { event_id: 'ev-2', event_time: '2026-07-08T13:40:00Z', operation: 'UPDATE',
      classes: ['scale'], actor_type: 'serviceaccount', user_name: 'system:serviceaccount:ci:d' },
  ],
}

function renderList() {
  render(
    <MemoryRouter>
      <SuspectList suspects={[suspect]} />
    </MemoryRouter>,
  )
}

test('renders score, resource identity, and reason chips', () => {
  renderList()
  expect(screen.getByText('87')).toBeInTheDocument()
  expect(screen.getByText('checkout')).toBeInTheDocument()
  expect(screen.getByText('payments')).toBeInTheDocument()
  expect(screen.getByText('Deployment')).toBeInTheDocument()
  expect(screen.getByText('image change')).toBeInTheDocument()
  expect(screen.getByText('first change in 12d')).toBeInTheDocument()
})

test('expanding a row reveals event links to the detail page', async () => {
  renderList()
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: /2 changes/i }))
  const links = screen.getAllByRole('link')
  expect(links[0]).toHaveAttribute('href', '/events/ev-1')
  expect(links[1]).toHaveAttribute('href', '/events/ev-2')
})
