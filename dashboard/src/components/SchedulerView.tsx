import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'

export function SchedulerView() {
  const [count, setCount] = useState(0)
  const [history, setHistory] = useState<api.JobRecord[]>([])
  const [loading, setLoading] = useState(true)

  const fetchData = useCallback(async () => {
    try {
      const [j, h] = await Promise.all([api.getSchedulerJobs(), api.getSchedulerHistory(100)])
      setCount(j.count)
      setHistory(h.history)
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  if (loading) return <div className="loading">加载中...</div>

  return (
    <div className="section">
      <h2>计划任务（{count}）</h2>
      {history.length === 0 && <p className="empty">暂无执行记录</p>}
      {history.map((rec, i) => (
        <div key={i} className="card">
          <div className="card-header">
            <span className={`status-dot ${rec.success ? 'running' : 'stopped'}`} />
            <strong>{rec.job_name}</strong>
            <span className="tag">{rec.success ? '成功' : '失败'}</span>
          </div>
          <div className="card-body">
            <div className="info-row"><span className="label">执行时间</span><span>{new Date(rec.start_at).toLocaleString()}</span></div>
            <div className="info-row"><span className="label">耗时</span><span>{rec.duration}</span></div>
            {rec.error && <div className="info-row error-text"><span className="label">错误</span><span>{rec.error}</span></div>}
          </div>
        </div>
      ))}
    </div>
  )
}
