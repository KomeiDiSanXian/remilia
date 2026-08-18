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
      let res = await fetch(`${base}/api/v1/version`)
      if (res.ok) {
        setNoKeyMode(true)
        setTesting(false)
        return
      }
      if (key.trim()) {
        res = await fetch(`${base}/api/v1/version`, {
          headers: { Authorization: `Bearer ${key.trim()}` },
        })
      }
      if (res.ok) {
        setNoKeyMode(false)
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
