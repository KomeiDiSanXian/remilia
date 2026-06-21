import { useEffect, useState, useRef, useCallback } from 'react'
import * as api from '../api'

export function LogViewer() {
  const [logs, setLogs] = useState<api.LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [paused, setPaused] = useState(false)
  const [levelFilter, setLevelFilter] = useState('')
  const timerRef = useRef<ReturnType<typeof setInterval>>(undefined)

  const fetchLogs = useCallback(async () => {
    try {
      const data = await api.getLogs(200)
      setLogs(data.entries)
    } catch {
      // ignore
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchLogs()
    timerRef.current = setInterval(fetchLogs, 2000)
    return () => clearInterval(timerRef.current)
  }, [fetchLogs])

  const containerRef = useRef<HTMLDivElement>(null)
  const initialLoad = useRef(true)

  // 首次加载强制滚到底部；后续仅在用户位于底部附近时跟随
  useEffect(() => {
    const el = containerRef.current
    if (paused || !el) return
    if (initialLoad.current) {
      initialLoad.current = false
      el.scrollTop = el.scrollHeight
      return
    }
    const threshold = 80
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
    if (atBottom) el.scrollTop = el.scrollHeight
  }, [logs, paused])

  const filtered = levelFilter
    ? logs.filter((e) => e.level === levelFilter)
    : logs

  const levelCounts: Record<string, number> = {}
  for (const e of logs) {
    levelCounts[e.level] = (levelCounts[e.level] || 0) + 1
  }

  return (
    <div className="section">
      <div className="section-header">
        <h2>后端日志</h2>
        <div className="log-toolbar">
          {['error', 'warn', 'info', 'debug'].map((lvl) => (
            <button
              key={lvl}
              className={`log-filter-btn ${levelFilter === lvl ? 'active' : ''} ${lvl}`}
              onClick={() => setLevelFilter(levelFilter === lvl ? '' : lvl)}
            >
              {lvl} ({levelCounts[lvl] || 0})
            </button>
          ))}
          <button className="btn-secondary" onClick={() => setPaused(!paused)}>
            {paused ? '继续' : '暂停'}
          </button>
          <button className="btn-secondary" onClick={fetchLogs}>刷新</button>
        </div>
      </div>

      {loading && <div className="loading">加载中...</div>}

      <div className="log-container" ref={containerRef}>
        {filtered.map((entry, i) => (
          <div key={i} className={`log-line log-${entry.level}`}>
            <span className="log-time">{entry.time}</span>
            <span className={`log-level log-${entry.level}`}>{entry.level}</span>
            <span className="log-msg">{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
