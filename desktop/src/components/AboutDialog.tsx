import { useCallback, useEffect, useState } from 'react'
import { getVersion } from '@tauri-apps/api/app'
import { check, type Update } from '@tauri-apps/plugin-updater'
import { relaunch } from '@tauri-apps/plugin-process'
import { ConfirmDialog } from './ConfirmDialog'

interface AboutDialogProps {
  open: boolean
  onClose: () => void
}

// 更新状态机：idle → checking → latest（已是最新）/ available（有新版本）
// available → downloading（带进度）→ ready（等用户重启生效）
// 任意阶段出错 → error。
type UpdateState =
  | { kind: 'idle' }
  | { kind: 'checking' }
  | { kind: 'latest' }
  | { kind: 'available'; update: Update }
  | { kind: 'downloading'; percent: number }
  | { kind: 'ready' }
  | { kind: 'error'; message: string }

export function AboutDialog({ open, onClose }: AboutDialogProps) {
  const [version, setVersion] = useState('')
  const [state, setState] = useState<UpdateState>({ kind: 'idle' })

  useEffect(() => {
    if (!open) return
    getVersion().then(setVersion).catch(() => setVersion('未知'))
  }, [open])

  const handleCheck = useCallback(async () => {
    // 开发模式下安装正式包会替换掉正在调试的版本，这里直接跳过
    if (import.meta.env.DEV) {
      setState({ kind: 'error', message: '开发模式不检查更新，请使用正式安装包' })
      return
    }
    setState({ kind: 'checking' })
    try {
      const update = await check()
      setState(update ? { kind: 'available', update } : { kind: 'latest' })
    } catch (e) {
      // 离线、更新源不可达或签名公钥未配置时都会抛错
      setState({ kind: 'error', message: String(e) })
    }
  }, [])

  const handleInstall = useCallback(async (update: Update) => {
    setState({ kind: 'downloading', percent: 0 })
    let total = 0
    let downloaded = 0
    try {
      await update.downloadAndInstall((event) => {
        if (event.event === 'Started') {
          total = event.data.contentLength ?? 0
        } else if (event.event === 'Progress') {
          downloaded += event.data.chunkLength
          setState({
            kind: 'downloading',
            percent: total > 0 ? Math.round((downloaded / total) * 100) : 0,
          })
        }
      })
      setState({ kind: 'ready' })
    } catch (e) {
      setState({ kind: 'error', message: String(e) })
    }
  }, [])

  const renderUpdateSection = () => {
    switch (state.kind) {
      case 'checking':
        return <p className="about-update-status">正在检查更新…</p>
      case 'latest':
        return (
          <>
            <p className="about-update-status">已是最新版本</p>
            <button className="about-update-btn" onClick={handleCheck}>重新检查</button>
          </>
        )
      case 'available':
        return (
          <>
            <p className="about-update-status">发现新版本 {state.update.version}</p>
            <button
              className="about-update-btn about-update-btn-primary"
              onClick={() => handleInstall(state.update)}
            >
              下载并安装
            </button>
          </>
        )
      case 'downloading':
        return (
          <div className="about-update-progress">
            <div className="about-update-progress-track">
              <div className="about-update-progress-bar" style={{ width: `${state.percent}%` }} />
            </div>
            <span className="about-update-status">下载中 {state.percent}%</span>
          </div>
        )
      case 'ready':
        return (
          <>
            <p className="about-update-status">更新已安装，重启后生效</p>
            <button
              className="about-update-btn about-update-btn-primary"
              onClick={() => { relaunch() }}
            >
              立即重启
            </button>
          </>
        )
      case 'error':
        return (
          <>
            <p className="about-update-status about-update-error">检查更新失败：{state.message}</p>
            <button className="about-update-btn" onClick={handleCheck}>重试</button>
          </>
        )
      default:
        return <button className="about-update-btn" onClick={handleCheck}>检查更新</button>
    }
  }

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
        <p className="about-version">版本 {version || '…'}</p>
        <p className="about-desc">
          Remilia 是一款通用聊天机器人框架，支持多平台接入与插件扩展。
        </p>
        <div className="about-update">{renderUpdateSection()}</div>
        <p className="about-link">
          <a href="https://github.com/KomeiDiSanXian/remilia" target="_blank" rel="noopener noreferrer">GitHub</a>
        </p>
      </div>
    </ConfirmDialog>
  )
}
