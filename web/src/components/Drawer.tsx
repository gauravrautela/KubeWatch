import { useEffect, useRef, type ReactNode } from 'react'

export function Drawer({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  const ref = useRef<HTMLElement>(null)
  useEffect(() => {
    ref.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="drawer-overlay" onClick={onClose}>
      <aside ref={ref} tabIndex={-1} className="drawer" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <button className="drawer-close" aria-label="close" onClick={onClose}>×</button>
        {children}
      </aside>
    </div>
  )
}
