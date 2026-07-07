import { test, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, useSearchParams } from 'react-router-dom'
import { CellFilter } from './CellFilter'

function Harness({ children }: { children: React.ReactNode }) {
  const [params] = useSearchParams()
  return (
    <>
      {children}
      <span data-testid="qs">{decodeURIComponent(params.toString())}</span>
    </>
  )
}

function renderCell(initial: string, ui: React.ReactNode, onRow = vi.fn()) {
  render(
    <MemoryRouter initialEntries={[initial]}>
      <Harness>
        <div onClick={onRow}>{ui}</div>
      </Harness>
    </MemoryRouter>,
  )
  return onRow
}

test('include click sets the single-value param', async () => {
  renderCell('/', <CellFilter field="kind" value="Deployment">Deployment</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'include kind Deployment' }))
  expect(screen.getByTestId('qs').textContent).toBe('kind=Deployment')
})

test('exclude click appends to the CSV param', async () => {
  renderCell('/?exclude_kinds=Lease', <CellFilter field="kind" value="Endpoints">Endpoints</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'exclude kind Endpoints' }))
  expect(screen.getByTestId('qs').textContent).toBe('exclude_kinds=Lease,Endpoints')
})

test('excluding a value clears a matching include', async () => {
  renderCell('/?kind=Deployment', <CellFilter field="kind" value="Deployment">Deployment</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'exclude kind Deployment' }))
  expect(screen.getByTestId('qs').textContent).toBe('exclude_kinds=Deployment')
})

test('including a value removes it from the exclude list', async () => {
  renderCell('/?exclude_users=bot,alice', <CellFilter field="user" value="alice">alice</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'include user alice' }))
  expect(screen.getByTestId('qs').textContent).toBe('exclude_users=bot&user=alice')
})

test('button clicks do not bubble to the row', async () => {
  const onRow = renderCell('/', <CellFilter field="cluster" value="c1">c1</CellFilter>)
  await userEvent.click(screen.getByRole('button', { name: 'include cluster c1' }))
  await userEvent.click(screen.getByRole('button', { name: 'exclude cluster c1' }))
  expect(onRow).not.toHaveBeenCalled()
})

test('renders children without buttons when value is empty', () => {
  renderCell('/', <CellFilter field="namespace" value="">—</CellFilter>)
  expect(screen.getByText('—')).toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})
