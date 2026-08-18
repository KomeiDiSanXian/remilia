import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'
import { Icon } from './Icons.tsx'

type FilterMode = 'all' | 'user' | 'action'

export function AuditLogView() {
  const [log, setLog] = useState<api.AuditLogResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [mode, setMode] = useState<FilterMode>('all')
  const [query, setQuery] = useState('')
  const [searchText, setSearchText] = useState('')

  const fetchData = useCallback(async () => {
    setLoading(true)
    try {
      let data: api.AuditLogResponse
      if (mode === 'user' && query.trim()) {
        data = await api.getAuditLogByUser(query.trim(), 100)
      } else if (mode === 'action' && query.trim()) {
        data = await api.getAuditLogByAction(query.trim(), 100)
      } else {
        data = await api.getAuditLog(100)
      }
      setLog(data)
      setError('')
    } catch (e: unknown) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [mode, query])

  useEffect(() => { fetchData() }, [fetchData])

  const applyFilter = () => {
    setQuery(searchText)
  }

  const filtered = log?.entries?.filter((entry) => {
    if (!searchText.trim()) return true
    const q = searchText.toLowerCase()
    return (entry.user_id || '').toLowerCase().includes(q)
      || (entry.action || '').toLowerCase().includes(q)
      || (entry.content || '').toLowerCase().includes(q)
  })

  return (
    <div className="section">
      <div className="section-header">
        <h2>审计日志 {log?.total !== undefined ? `（共 ${log.total} 条）` : ''}</h2>
        <button className="btn-secondary" onClick={fetchData}><Icon name="refresh" size={13} />刷新</button>
      </div>

      <div className="plugin-toolbar">
        <div className="tab-bar" style={{ marginBottom: 0 }}>
          {([['all', '全部'], ['user', '按用户'], ['action', '按动作']] as [FilterMode, string][]).map(([m, label]) => (
            <button key={m} className={mode === m ? 'active' : ''} onClick={() => { setMode(m); setSearchText(''); setQuery('') }}>
              {label}
            </button>
          ))}
        </div>
        <div className="plugin-search">
          <Icon name="search" size={14} />
          <input
            type="text"
            value={searchText}
            onChange={(e) => setSearchText(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') applyFilter() }}
            placeholder={mode === 'user' ? '输入用户 ID 后回车筛选' : mode === 'action' ? '输入动作名称后回车筛选' : '搜索用户 / 动作 / 内容'}
            spellCheck={false}
          />
        </div>
        <button className="btn-secondary" onClick={applyFilter}>筛选</button>
      </div>

      {loading && <div className="loading">加载中...</div>}
      {error && <div className="error-card">获取失败: {error}</div>}

      {!loading && !error && (!filtered || filtered.length === 0) && <p className="empty">暂无日志</p>}

      {!loading && !error && filtered && filtered.length > 0 && (
        <div className="card">
          <table className="data-table audit-table">
            <thead>
              <tr><th>时间</th><th>动作</th><th>用户</th><th>内容</th></tr>
            </thead>
            <tbody>
              {filtered.map((entry) => (
                <tr key={entry.id}>
                  <td>{new Date(entry.timestamp).toLocaleString()}</td>
                  <td><span className="chip info">{entry.action}</span></td>
                  <td className="mono">{entry.user_id}</td>
                  <td>
                    {entry.content && <div className="audit-content">{entry.content}</div>}
                    <div className="audit-meta">
                      {entry.group_id && <span>群组: {entry.group_id}</span>}
                      {entry.meta && <span> meta: {JSON.stringify(entry.meta)}</span>}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
