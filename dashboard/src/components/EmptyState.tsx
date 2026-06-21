import type { ReactNode } from 'react'

interface EmptyStateProps {
  message: string
  hint?: string
  action?: { label: string; onClick: () => void }
  children?: ReactNode
}

export function EmptyState({ message, hint, action, children }: EmptyStateProps) {
  return (
    <div className="empty-state">
      <div className="empty-state-icon">◌</div>
      <p className="empty-state-msg">{message}</p>
      {hint && <p className="empty-state-hint">{hint}</p>}
      {action && <button className="btn-secondary" onClick={action.onClick}>{action.label}</button>}
      {children}
    </div>
  )
}
