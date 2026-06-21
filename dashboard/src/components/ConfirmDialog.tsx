import { useEffect, useRef } from 'react'

interface ConfirmDialogProps {
  open: boolean
  title: string
  message: string
  confirmLabel?: string
  confirmVariant?: 'danger' | 'primary'
  loading?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({ open, title, message, confirmLabel = '确认', confirmVariant = 'danger', loading, onConfirm, onCancel }: ConfirmDialogProps) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onCancel() }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [open, onCancel])

  if (!open) return null

  return (
    <div className="dialog-overlay" onClick={onCancel}>
      <div className="dialog confirm-dialog" ref={ref} onClick={(e) => e.stopPropagation()}>
        <div className="dialog-header">{title}</div>
        <div className="dialog-body">{message}</div>
        <div className="dialog-actions">
          <button className="btn-secondary" onClick={onCancel} disabled={loading}>取消</button>
          <button className={confirmVariant === 'danger' ? 'warn' : ''} onClick={onConfirm} disabled={loading}>
            {loading ? `${confirmLabel}中...` : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
