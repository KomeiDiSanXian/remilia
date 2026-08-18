import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'
import { useToast } from './Toast.tsx'
import { Icon } from './Icons.tsx'

export function MatcherView() {
  const [stats, setStats] = useState<api.MatcherStats | null>(null)
  const [groups, setGroups] = useState<api.MatcherGroupInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [toggling, setToggling] = useState('')
  const { toast } = useToast()

  const fetchData = useCallback(async () => {
    try {
      const [s, g] = await Promise.all([api.getMatcherStats(), api.listMatcherGroups()])
      setStats(s)
      setGroups(g)
      setError('')
    } catch (e: unknown) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const toggleGroup = useCallback(async (name: string, enabled: boolean) => {
    setToggling(name)
    try {
      if (enabled) await api.disableMatcherGroup(name)
      else await api.enableMatcherGroup(name)
      toast(`分组「${name}」${enabled ? '已禁用' : '已启用'}`, 'success')
      fetchData()
    } catch (e: unknown) {
      toast(`操作失败: ${(e as Error).message}`, 'error')
    } finally {
      setToggling('')
    }
  }, [fetchData, toast])

  if (loading) return <div className="loading">加载中...</div>
  if (error) return <div className="error-card">获取失败: {error}</div>
  if (!stats) return null

  const byPlugin = Object.entries(stats.by_plugin || {}).sort((a, b) => b[1] - a[1])
  const maxPlugin = byPlugin[0]?.[1] || 1

  return (
    <div className="section">
      <div className="section-header">
        <h2>匹配器统计</h2>
        <button className="btn-secondary" onClick={fetchData}><Icon name="refresh" size={13} />刷新</button>
      </div>

      <div className="stat-grid">
        <div className="stat-card">
          <span className="stat-icon"><Icon name="matcher" size={18} /></span>
          <div className="stat-body"><div className="stat-value">{stats.total}</div><div className="stat-label">匹配器总数</div></div>
        </div>
        <div className="stat-card">
          <span className="stat-icon blue"><Icon name="zap" size={18} /></span>
          <div className="stat-body"><div className="stat-value">{stats.global}</div><div className="stat-label">全局匹配器</div></div>
        </div>
        <div className="stat-card">
          <span className="stat-icon amber"><Icon name="clock" size={18} /></span>
          <div className="stat-body"><div className="stat-value">{stats.temp}</div><div className="stat-label">临时匹配器</div></div>
        </div>
        <div className="stat-card">
          <span className={`stat-icon ${stats.global_enabled ? 'green' : 'red'}`}><Icon name={stats.global_enabled ? 'check' : 'stop'} size={18} /></span>
          <div className="stat-body"><div className="stat-value">{stats.global_enabled ? '启用' : '停用'}</div><div className="stat-label">全局匹配开关</div></div>
        </div>
      </div>

      {groups.length > 0 && (
        <>
          <div className="section-header" style={{ marginTop: '1.2rem' }}>
            <h2><Icon name="filter" size={18} />匹配器分组</h2>
            <span className="plugin-count">{groups.length} 个分组</span>
          </div>
          <div className="card">
            {groups.map((g, i) => (
              <div className="job-row" key={g.name} style={i === 0 ? { borderTop: 'none' } : undefined}>
                <span className={`status-dot ${g.enabled ? 'running' : 'stopped'}`} />
                <span className="job-name">{g.name}</span>
                <span className="chip info"><Icon name="matcher" size={11} />{g.count}</span>
                <span className={`chip ${g.enabled ? 'success' : 'warning'}`}>{g.enabled ? '已启用' : '已禁用'}</span>
                <button
                  className="btn-secondary btn-sm"
                  onClick={() => toggleGroup(g.name, g.enabled)}
                  disabled={toggling === g.name}
                >
                  {g.enabled ? '禁用' : '启用'}
                </button>
              </div>
            ))}
          </div>
        </>
      )}

      {byPlugin.length > 0 && (
        <>
          <div className="section-header" style={{ marginTop: '1.2rem' }}>
            <h2><Icon name="plugin" size={18} />按插件分布</h2>
          </div>
          <div className="card">
            {byPlugin.map(([plugin, count]) => (
              <div key={plugin} style={{ marginBottom: '0.7rem' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '0.82rem', marginBottom: '0.25rem' }}>
                  <span>{plugin}</span>
                  <span className="plugin-count">{count}</span>
                </div>
                <div style={{ height: 6, background: 'var(--surface-3)', borderRadius: 999, overflow: 'hidden' }}>
                  <div
                    style={{
                      height: '100%', width: `${Math.max(3, (count / maxPlugin) * 100)}%`,
                      borderRadius: 999,
                      background: 'linear-gradient(90deg, var(--accent), var(--blue))',
                    }}
                  />
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
