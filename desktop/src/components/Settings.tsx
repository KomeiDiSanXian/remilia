import { useState } from 'react'
import { Icon } from '@dashboard/components/Icons'

interface SettingsProps {
  initialUrl?: string
  onConnect: (url: string, key: string) => void
}

export function Settings({ initialUrl = 'http://localhost:9002', onConnect }: SettingsProps) {
  const [url, setUrl] = useState(initialUrl)
  const [key, setKey] = useState('')
  const [error, setError] = useState('')
  const [testing, setTesting] = useState(false)
  const [noKeyMode, setNoKeyMode] = useState(false)

  const handleTest = async () => {
    setTesting(true)
    setError('')
    try {
      const base = url.replace(/\/+$/, '')
      // 探测受保护端点：公开的 /version 无法区分"服务未配置 Key"和"Key 无效"。
      let res = await fetch(`${base}/api/v1/auth/verify`)
      if (res.ok) {
        setNoKeyMode(true)
        setError('')
        return
      }
      if (res.status !== 401) {
        setError(`连接失败 (HTTP ${res.status})`)
        return
      }
      setNoKeyMode(false)
      if (!key.trim()) {
        setError('该服务需要 API Key')
        return
      }
      res = await fetch(`${base}/api/v1/auth/verify`, {
        headers: { Authorization: `Bearer ${key.trim()}` },
      })
      if (res.ok) {
        setError('')
      } else if (res.status === 401) {
        setError('API Key 无效')
      } else {
        setError(`连接失败 (HTTP ${res.status})`)
      }
    } catch (e: unknown) {
      setError('连接失败: ' + (e as Error).message)
    } finally {
      setTesting(false)
    }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    onConnect(url.replace(/\/+$/, ''), key.trim())
  }

  return (
    <div className="settings-container">
      <div className="settings-card">
        <div className="login-logo">R</div>
        <h1>连接到 Bot</h1>
        <p className="subtitle">输入 Remilia 管理 API 地址和密钥</p>
        <form onSubmit={handleSubmit}>
          <div className="field">
            <label htmlFor="url">API 地址</label>
            <div className="field-row">
              <input
                id="url"
                type="text"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="http://localhost:9002"
              />
              <button type="button" className="btn-secondary" onClick={handleTest} disabled={testing}>
                {testing ? '测试中...' : '测试'}
              </button>
            </div>
          </div>
          <div className="field">
            <label htmlFor="key">
              API Key
              {noKeyMode && <span className="key-hint">（可选 — 开发模式无需 Key）</span>}
            </label>
            <input
              id="key"
              type="password"
              value={key}
              onChange={(e) => { setKey(e.target.value); setError('') }}
              placeholder={noKeyMode ? '留空以跳过认证' : '输入 API Key'}
            />
          </div>
          {error && <p className="error">{error}</p>}
          <button type="submit"><Icon name="link" size={15} />连接</button>
        </form>
      </div>
    </div>
  )
}
