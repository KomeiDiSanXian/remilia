import { useState } from 'react'
import { invoke } from '@tauri-apps/api/core'
import { ConfirmDialog } from './ConfirmDialog'

const CLOSE_PREF_KEY = 'remilia_close_preference'

interface TitleBarProps {
  title?: string
}

export function TitleBar({ title = 'Remilia Desktop' }: TitleBarProps) {
  const [showCloseDialog, setShowCloseDialog] = useState(false)
  const [rememberClose, setRememberClose] = useState(false)

  const handleMinimize = () => {
    invoke('minimize_window')
  }

  const handleCloseClick = () => {
    const pref = localStorage.getItem(CLOSE_PREF_KEY)
    if (pref === 'hide') {
      invoke('close_window')
      return
    }
    if (pref === 'quit') {
      invoke('quit_app')
      return
    }
    setShowCloseDialog(true)
  }

  const handleHideToTray = () => {
    if (rememberClose) localStorage.setItem(CLOSE_PREF_KEY, 'hide')
    setShowCloseDialog(false)
    invoke('close_window')
  }

  const handleQuit = () => {
    if (rememberClose) localStorage.setItem(CLOSE_PREF_KEY, 'quit')
    setShowCloseDialog(false)
    invoke('quit_app')
  }

  return (
    <>
      <div className="titlebar" data-tauri-drag-region>
        <span className="titlebar-title">{title}</span>
        <div className="titlebar-controls">
          <button className="titlebar-btn" onClick={handleMinimize} title="最小化">
            <svg width="12" height="12" viewBox="0 0 12 12">
              <rect x="2" y="5.5" width="8" height="1" fill="currentColor" />
            </svg>
          </button>
          <button className="titlebar-btn titlebar-close" onClick={handleCloseClick} title="关闭">
            <svg width="12" height="12" viewBox="0 0 12 12">
              <path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" strokeWidth="1.2" />
            </svg>
          </button>
        </div>
      </div>

      <ConfirmDialog
        open={showCloseDialog}
        title="关闭 Remilia Desktop"
        remember={{ label: '记住此选项，下次不再询问', checked: rememberClose, onChange: setRememberClose }}
        actions={[
          { label: '隐藏到托盘', onClick: handleHideToTray },
          { label: '完全关闭', onClick: handleQuit, variant: 'danger' },
        ]}
        onClose={() => setShowCloseDialog(false)}
      >
        <p>请选择关闭方式：</p>
      </ConfirmDialog>
    </>
  )
}
