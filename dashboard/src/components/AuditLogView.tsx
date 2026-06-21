import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'

export function AuditLogView() {
  const [log, setLog] = useState<api.AuditLogResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchData = useCallback(async () => {
    try {
      const data = await api.getAuditLog(100)
      setLog(data)
      setError('')
    } catch (e: unknown) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  if (loading) return <div className="loading">加载中...</div>
  if (error) return <div className="error-card">获取失败: {error}</div>

  return (
    <div className="section">
      <div className="section-header">
        <h2>审计日志 {log?.total !== undefined ? `（共 ${log.total} 条）` : ''}</h2>
        <button className="refresh" onClick={fetchData}>刷新</button>
      </div>

      {!log?.entries?.length && <p className="empty">暂无日志</p>}

      {log?.entries?.map((entry) => (
        <div key={entry.id} className="card log-entry">
          <div className="card-header">
            <span className="tag">{entry.action}</span>
            <span>{entry.user_id}</span>
            <span className="log-time">{new Date(entry.timestamp).toLocaleString()}</span>
          </div>
          <div className="card-body">
            {entry.content && <p className="log-content">{entry.content}</p>}
            {entry.group_id && (
              <div className="info-row">
                <span className="label">群组</span>
                <span>{entry.group_id}</span>
              </div>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}
