import { useState } from 'react'
import { getStoredApiKey, setStoredApiKey, clearApiKey, setBaseURL } from './api'
import { Login } from './components/Login.tsx'
import { Dashboard } from './components/Dashboard.tsx'

const URL_STORAGE_KEY = 'remilia_api_url'
const SESSION_KEY = 'remilia_logged_in'

export default function App() {
  // 会话标记独立于 Key 存储：空 Key（开发模式）也要能登录并保持会话，
  // 之前用 apiKey 的真值做判断，留空 Key 的提交会被 Login 重新渲染吞掉。
  const [loggedIn, setLoggedIn] = useState<boolean>(() => {
    const savedUrl = localStorage.getItem(URL_STORAGE_KEY)
    if (!savedUrl) return false
    const savedKey = getStoredApiKey()
    if (savedKey || localStorage.getItem(SESSION_KEY) === '1') {
      setBaseURL(savedUrl)
      return true
    }
    return false
  })

  const handleLogin = (key: string, url: string) => {
    setBaseURL(url)
    localStorage.setItem(URL_STORAGE_KEY, url)
    // request() 每次从 localStorage 读取密钥；不落盘的话认证请求不会携带
    // Authorization 头，配置了 api.api_key 的服务会 401，刷新后还会退回登录页。
    if (key) {
      setStoredApiKey(key)
    } else {
      clearApiKey()
    }
    localStorage.setItem(SESSION_KEY, '1')
    setLoggedIn(true)
  }

  const handleLogout = () => {
    clearApiKey()
    localStorage.removeItem(URL_STORAGE_KEY)
    localStorage.removeItem(SESSION_KEY)
    setLoggedIn(false)
  }

  if (!loggedIn) {
    return <Login onLogin={handleLogin} />
  }

  return <Dashboard onLogout={handleLogout} />
}
