import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'

export function FSMView() {
  const [fsms, setFsms] = useState<string[]>([])
  const [loading, setLoading] = useState(true)

  const fetchData = useCallback(async () => {
    try {
      const d = await api.listFSMs()
      setFsms(d.fsms)
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  if (loading) return <div className="loading">加载中...</div>

  return (
    <div className="section">
      <h2>FSM 状态机</h2>
      {fsms.length === 0 && <p className="empty">没有已注册的 FSM</p>}
      {fsms.map((name) => (
        <div key={name} className="card">
          <div className="card-header"><strong>{name}</strong></div>
        </div>
      ))}
    </div>
  )
}
