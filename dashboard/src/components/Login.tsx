import { useState } from 'react'
import { Icon } from './Icons.tsx'

interface LoginProps {
  onLogin: (apiKey: string, url: string) => void
}

export function Login({ onLogin }: LoginProps) {
  const [url, setUrl] = useState(() => localStorage.getItem('remilia_api_url') || 'http://localhost:9002')
  const [key, setKey] = useState('')
  const [error, setError] = useState('')
  const [testing, setTesting] = useState(false)
  const [noKeyMode, setNoKeyMode] = useState(false)

  const handleTest = async () => {
    setTesting(true)
    setError('')
    try {
      const base = url.replace(/\/+$/, '')
      // 探测受保护端点：公开的 /version 无法区分"服务未配置 Key"和"Key 无效"，
      // 之前用 /version 探测会永远显示"开发模式无需 Key"。
      let res = await fetch(`${base}/api/v1/auth/verify`)
      if (res.ok) {
        setNoKeyMode(true)
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
    onLogin(key.trim(), url.replace(/\/+$/, ''))
  }

  return (
    <div className="login-container">
      <div className="login-card">
        <div className="login-logo">R</div>
        <h1>Remilia Dashboard</h1>
        <p className="subtitle">连接管理 API 以管理你的机器人实例</p>
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
          <button type="submit">
            <Icon name="link" size={15} />连接
          </button>
        </form>
      </div>
    </div>
  )
}
