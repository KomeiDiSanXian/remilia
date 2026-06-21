import { useState, useEffect, useCallback } from 'react'
import { TitleBar } from './components/TitleBar.tsx'
import { Settings } from './components/Settings.tsx'
import { AboutDialog } from './components/AboutDialog.tsx'
import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import { getCurrentWebviewWindow } from '@tauri-apps/api/webviewWindow'
import { setBaseURL, setStoredApiKey, clearApiKey, getStoredApiKey } from '@dashboard/api'
import { Dashboard } from '@dashboard/components/Dashboard'

const URL_STORAGE_KEY = 'remilia_api_url'
const SIDECAR_URL = 'http://localhost:9002'

async function tryLocalSidecar(): Promise<{ url: string; key: string } | null> {
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const controller = new AbortController()
      const timer = setTimeout(() => controller.abort(), 1500)
      const res = await fetch(`${SIDECAR_URL}/api/v1/version`, { signal: controller.signal })
      clearTimeout(timer)
      if (res.ok || res.status === 401) return { url: SIDECAR_URL, key: '' }
    } catch { }
    await new Promise((r) => setTimeout(r, 400))
  }
  return null
}

type StartupChoice = 'choose' | 'starting-local' | 'connecting'

export default function App() {
  const [connected, setConnected] = useState(false)
  const [loading, setLoading] = useState(true)
  const [aboutOpen, setAboutOpen] = useState(false)
  const [startupChoice, setStartupChoice] = useState<StartupChoice>('choose')
  const [localError, setLocalError] = useState('')

  useEffect(() => {
    ;(async () => {
      try {
        const win = getCurrentWebviewWindow()
        await win.show()
        await win.setFocus()
      } catch { }
      // 检查有没有已保存的远程连接
      const savedUrl = localStorage.getItem(URL_STORAGE_KEY)
      const savedKey = getStoredApiKey()
      if (savedUrl && savedKey) {
        setBaseURL(savedUrl)
        setStoredApiKey(savedKey)
        try { await invoke('set_connection', { config: { url: savedUrl, key: savedKey } }) } catch { }
        setConnected(true)
        setStartupChoice('connecting')
      }
      setLoading(false)
    })()
  }, [])

  const handleStartLocal = useCallback(async () => {
    setStartupChoice('starting-local')
    setLocalError('')
    try {
      const ok = await invoke<boolean>('start_bot')
      if (!ok) { setLocalError('启动本地后端失败，请检查 sidecar 是否存在'); return }
      setStartupChoice('connecting')
    } catch (e) { setLocalError(String(e)); return }

    // 等待后端就绪
    for (let i = 0; i < 10; i++) {
      const sidecar = await tryLocalSidecar()
      if (sidecar) {
        setBaseURL(sidecar.url)
        localStorage.removeItem(URL_STORAGE_KEY)
        clearApiKey()
        try { await invoke('set_connection', { config: { url: sidecar.url, key: sidecar.key } }) } catch { }
        setConnected(true)
        return
      }
      await new Promise((r) => setTimeout(r, 800))
    }
    setLocalError('本地后端启动超时，请稍后重试')
    setStartupChoice('choose')
  }, [])

  const handleConnectRemote = useCallback(() => {
    setStartupChoice('connecting')
  }, [])

  useEffect(() => {
    const unlisten = listen<string>('navigate', (event) => {
      if (event.payload === 'about') {
        setAboutOpen(true)
      } else if (event.payload === 'settings') {
        setConnected(false)
        setStartupChoice('choose')
      }
    })
    return () => { unlisten.then((fn) => fn()) }
  }, [])

  // 窗口位置自动保存（移除以兼容 Tauri 版本差异，保留 save/load 命令供手动调用）

  const handleDisconnect = useCallback(() => {
    clearApiKey()
    localStorage.removeItem(URL_STORAGE_KEY)
    invoke('set_connection', { config: { url: '', key: '' } }).catch(() => {})
    setConnected(false)
    setStartupChoice('choose')
    setLocalError('')
  }, [])

  if (loading) {
    return (
      <div className="app">
        <TitleBar />
        <div className="loading-screen">
          <div className="loading-spinner" />
          <span>加载中...</span>
        </div>
      </div>
    )
  }

  if (!connected) {
    if (startupChoice === 'choose') {
      return (
        <div className="app">
          <TitleBar />
          <div className="startup-choice">
            <h1>Remilia Desktop</h1>
            <p className="startup-subtitle">选择连接方式</p>
            {localError && <div className="banner error" style={{ marginBottom: '1rem' }}>{localError}</div>}
            <div className="startup-cards">
              <button className="startup-card" onClick={handleStartLocal}>
                <span className="startup-card-icon">🚀</span>
                <span className="startup-card-title">启动本地后端</span>
                <span className="startup-card-desc">自动启动内置 Go 后端并连接</span>
              </button>
              <button className="startup-card" onClick={handleConnectRemote}>
                <span className="startup-card-icon">🌐</span>
                <span className="startup-card-title">连接远程后端</span>
                <span className="startup-card-desc">连接到已运行的远程 Remilia 实例</span>
              </button>
            </div>
          </div>
        </div>
      )
    }
    if (startupChoice === 'starting-local' || startupChoice === 'connecting') {
      return (
        <div className="app">
          <TitleBar />
          <div className="settings-page">
            {startupChoice === 'connecting' && (
              <button className="back-btn" onClick={() => setStartupChoice('choose')}>← 返回</button>
            )}
            <Settings initialUrl={localStorage.getItem(URL_STORAGE_KEY) || SIDECAR_URL} onConnect={(url, key) => {
              setBaseURL(url)
              setStoredApiKey(key)
              localStorage.setItem(URL_STORAGE_KEY, url)
              invoke('set_connection', { config: { url, key } }).catch(() => {})
              setConnected(true)
            }} />
          </div>
        </div>
      )
    }
    return null
  }

  return (
    <div className="app">
      <TitleBar />
      <Dashboard onLogout={handleDisconnect} />
      <AboutDialog open={aboutOpen} onClose={() => setAboutOpen(false)} />
    </div>
  )
}
