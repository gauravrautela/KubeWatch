import { test, expect, vi } from 'vitest'
import { render, fireEvent, screen } from '@testing-library/react'
import { Drawer } from './Drawer'

test('Drawer focuses itself on mount and closes on Escape', () => {
  const onClose = vi.fn()
  render(
    <Drawer onClose={onClose}>
      <div>content</div>
    </Drawer>,
  )

  expect(screen.getByRole('dialog')).toHaveFocus()

  fireEvent.keyDown(document, { key: 'Escape' })
  expect(onClose).toHaveBeenCalledTimes(1)
})
