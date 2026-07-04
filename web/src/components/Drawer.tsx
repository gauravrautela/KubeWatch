import type { ReactNode } from 'react'

export function Drawer({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  return (
    <div className="drawer-overlay" onClick={onClose}>
      <aside className="drawer" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <button className="drawer-close" aria-label="close" onClick={onClose}>×</button>
        {children}
      </aside>
    </div>
  )
}
