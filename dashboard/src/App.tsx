import { useState } from 'react'
import { getStoredApiKey, clearApiKey, setBaseURL } from './api'
import { Login } from './components/Login.tsx'
import { Dashboard } from './components/Dashboard.tsx'

const URL_STORAGE_KEY = 'remilia_api_url'

export default function App() {
  const [apiKey, setApiKey] = useState<string | null>(() => {
    const saved = getStoredApiKey()
    if (saved) {
      const savedUrl = localStorage.getItem(URL_STORAGE_KEY)
      if (savedUrl) setBaseURL(savedUrl)
    }
    return saved
  })

  const handleLogin = (key: string, url: string) => {
    setBaseURL(url)
    localStorage.setItem(URL_STORAGE_KEY, url)
    setApiKey(key)
  }

  const handleLogout = () => {
    clearApiKey()
    localStorage.removeItem(URL_STORAGE_KEY)
    setApiKey(null)
  }

  if (!apiKey) {
    return <Login onLogin={handleLogin} />
  }

  return <Dashboard onLogout={handleLogout} />
}
