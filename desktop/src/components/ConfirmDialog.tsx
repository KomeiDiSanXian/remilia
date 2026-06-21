import { useEffect, useRef } from 'react'

interface ConfirmDialogProps {
  open: boolean
  title: string
  children: React.ReactNode
  remember?: { label: string; checked: boolean; onChange: (v: boolean) => void }
  actions: { label: string; onClick: () => void; variant?: 'primary' | 'danger' }[]
  onClose: () => void
}

export function ConfirmDialog({ open, title, children, remember, actions, onClose }: ConfirmDialogProps) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [open, onClose])

  if (!open) return null

  return (
    <div className="dialog-overlay" onClick={onClose}>
      <div className="dialog" ref={ref} onClick={(e) => e.stopPropagation()}>
        <div className="dialog-header">{title}</div>
        <div className="dialog-body">{children}</div>
        {remember && (
          <label className="dialog-remember">
            <input type="checkbox" checked={remember.checked} onChange={(e) => remember.onChange(e.target.checked)} />
            {remember.label}
          </label>
        )}
        <div className="dialog-actions">
          {actions.map((a) => (
            <button key={a.label} className={`dialog-btn ${a.variant === 'danger' ? 'dialog-btn-danger' : a.variant === 'primary' ? 'dialog-btn-primary' : ''}`} onClick={a.onClick}>
              {a.label}
            </button>
          ))}
        </div>
      </div>
    </div>
  )
}
