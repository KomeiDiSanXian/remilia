import { useEffect, useState, useRef, useCallback } from 'react'
import * as api from '../api'
import { getStoredApiKey, baseURL } from '../api'
import { Icon } from './Icons.tsx'

const MAX_ENTRIES = 500

export function LogViewer() {
  const [logs, setLogs] = useState<api.LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [paused, setPaused] = useState(false)
  const [levelFilter, setLevelFilter] = useState('')
  const [streaming, setStreaming] = useState(false)
  const timerRef = useRef<ReturnType<typeof setInterval>>(undefined)
  const abortRef = useRef<AbortController | null>(null)
  const pausedRef = useRef(paused)
  pausedRef.current = paused

  const appendEntries = useCallback((entries: api.LogEntry[]) => {
    setLogs((prev) => {
      const next = [...prev]
      for (const entry of entries) {
        const last = next[next.length - 1]
        // 去重：SSE 首帧会重发最近 50 条历史日志
        if (last && last.time === entry.time && last.level === entry.level && last.message === entry.message) {
          continue
        }
        next.push(entry)
      }
      return next.length > MAX_ENTRIES ? next.slice(next.length - MAX_ENTRIES) : next
    })
  }, [])

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

  // SSE 实时流（携带 Authorization 头，无法用 EventSource）
  const startStream = useCallback(() => {
    const apiKey = getStoredApiKey()
    const controller = new AbortController()
    abortRef.current = controller

    const connect = async () => {
      try {
        const res = await fetch(`${baseURL}/api/v1/logs/stream`, {
          signal: controller.signal,
          headers: apiKey ? { Authorization: `Bearer ${apiKey}` } : undefined,
        })
        if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`)
        setStreaming(true)
        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          let idx: number
          while ((idx = buffer.indexOf('\n\n')) >= 0) {
            const chunk = buffer.slice(0, idx)
            buffer = buffer.slice(idx + 2)
            for (const line of chunk.split('\n')) {
              if (!line.startsWith('data:')) continue
              try {
                const entry = JSON.parse(line.slice(5).trim()) as api.LogEntry
                if (!pausedRef.current) appendEntries([entry])
              } catch { /* 忽略无法解析的行 */ }
            }
          }
        }
      } catch {
        // 流中断（正常关闭 / 后端不支持）：回退到轮询
      } finally {
        setStreaming(false)
        if (!controller.signal.aborted) {
          timerRef.current = setInterval(() => { if (!pausedRef.current) fetchLogs() }, 2000)
        }
      }
    }
    connect()
  }, [appendEntries, fetchLogs])

  useEffect(() => {
    fetchLogs()
    startStream()
    return () => {
      abortRef.current?.abort()
      clearInterval(timerRef.current)
    }
  }, [fetchLogs, startStream])

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
        <h2>后端日志 {streaming && <span className="chip success"><Icon name="activity" size={11} />实时</span>}</h2>
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
            <Icon name={paused ? 'play' : 'pause'} size={13} />{paused ? '继续' : '暂停'}
          </button>
          <button className="btn-secondary" onClick={() => { abortRef.current?.abort(); clearInterval(timerRef.current); startStream() }}>
            <Icon name="refresh" size={13} />重连
          </button>
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
        {filtered.length === 0 && !loading && <div className="empty">暂无日志</div>}
      </div>
    </div>
  )
}
