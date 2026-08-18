import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'
import { useToast } from './Toast.tsx'
import { Icon } from './Icons.tsx'

interface FSMSummary {
  name: string
  initial: string
  timeout: string
}

export function FSMView() {
  const [fsms, setFsms] = useState<FSMSummary[]>([])
  const [sessions, setSessions] = useState<api.FSMSessionInfo[]>([])
  const [loading, setLoading] = useState(true)
  const { toast } = useToast()

  const fetchData = useCallback(async () => {
    try {
      const [list, sess] = await Promise.all([api.listFSMs(), api.listFSMSessions()])
      const summaries: FSMSummary[] = []
      for (const name of list.fsms) {
        try {
          const detail = await api.getFSMDetail(name)
          summaries.push(detail)
        } catch {
          summaries.push({ name, initial: '-', timeout: '-' })
        }
      }
      setFsms(summaries)
      setSessions(sess)
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const handleEnd = useCallback(async (id: string) => {
    try {
      await api.endFSMSession(id)
      toast('会话已结束', 'success')
      fetchData()
    } catch (e: unknown) {
      toast(`结束失败: ${(e as Error).message}`, 'error')
    }
  }, [fetchData, toast])

  if (loading) return <div className="loading">加载中...</div>

  return (
    <div className="section">
      <div className="section-header">
        <h2>FSM 状态机 <span className="plugin-count">({fsms.length})</span></h2>
        <button className="btn-secondary" onClick={fetchData}><Icon name="refresh" size={13} />刷新</button>
      </div>

      {fsms.length === 0 && <p className="empty">没有已注册的 FSM</p>}

      <div className="fsm-grid">
        {fsms.map((f) => (
          <div key={f.name} className="card fsm-card">
            <div className="card-header">
              <span className="status-dot running" />
              <strong>{f.name}</strong>
            </div>
            <div className="card-body">
              <div className="info-row"><span className="label">初始状态</span><span><code>{f.initial}</code></span></div>
              {f.timeout && f.timeout !== '0s' && f.timeout !== '-' && (
                <div className="info-row"><span className="label">超时</span><span>{f.timeout}</span></div>
              )}
            </div>
          </div>
        ))}
      </div>

      {sessions.length > 0 && (
        <>
          <div className="section-header" style={{ marginTop: '1.2rem' }}>
            <h2><Icon name="activity" size={18} />活跃会话 <span className="plugin-count">({sessions.length})</span></h2>
          </div>
          <div className="card">
            <table className="data-table session-table">
              <thead>
                <tr><th>会话 ID</th><th>FSM</th><th>当前状态</th><th>创建时间</th><th>操作</th></tr>
              </thead>
              <tbody>
                {sessions.map((s) => (
                  <tr key={s.id}>
                    <td className="mono" style={{ maxWidth: 220, overflow: 'hidden', textOverflow: 'ellipsis' }}>{s.id}</td>
                    <td>{s.fsm_name}</td>
                    <td><code>{s.current}</code></td>
                    <td>{new Date(s.created_at * 1000).toLocaleString()}</td>
                    <td>
                      <button className="btn-secondary btn-sm" onClick={() => handleEnd(s.id)}>
                        <Icon name="stop" size={12} />结束
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  )
}
