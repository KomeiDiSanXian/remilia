import { ConfirmDialog } from './ConfirmDialog'

interface AboutDialogProps {
  open: boolean
  onClose: () => void
}

export function AboutDialog({ open, onClose }: AboutDialogProps) {
  return (
    <ConfirmDialog
      open={open}
      title="关于 Remilia Desktop"
      actions={[{ label: '确定', onClick: onClose, variant: 'primary' }]}
      onClose={onClose}
    >
      <div className="about-content">
        <div className="about-logo">R</div>
        <p className="about-name">Remilia Desktop</p>
        <p className="about-version">版本 0.1.0</p>
        <p className="about-desc">
          Remilia 是一款通用聊天机器人框架，支持多平台接入与插件扩展。
        </p>
        <p className="about-link">
          <a href="https://github.com/KomeiDiSanXian/remilia" target="_blank" rel="noopener noreferrer">GitHub</a>
        </p>
      </div>
    </ConfirmDialog>
  )
}
