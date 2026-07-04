import { afterEach, test, expect, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ActivityHistogram } from './ActivityHistogram'

afterEach(() => vi.unstubAllGlobals())

function renderHist(body: unknown) {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(body) }))
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ActivityHistogram />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

test('shows an empty state when there is no activity', async () => {
  renderHist([])
  await waitFor(() => expect(screen.getByText(/no activity/i)).toBeInTheDocument())
})

test('renders the chart region when buckets exist', async () => {
  renderHist([{ bucket_start: '2026-07-01T10:00:00Z', count: 5 }])
  await waitFor(() => expect(screen.getByLabelText('activity histogram')).toBeInTheDocument())
})
