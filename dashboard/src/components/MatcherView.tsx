import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'

export function MatcherView() {
  const [stats, setStats] = useState<api.MatcherStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchData = useCallback(async () => {
    try {
      const s = await api.getMatcherStats()
      setStats(s)
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
  if (!stats) return null

  return (
    <div className="section">
      <h2>匹配器统计</h2>
      <div className="card">
        <div className="card-body">
          <div className="info-row">
            <span className="label">总计</span>
            <span>{stats.total}</span>
          </div>
          <div className="info-row">
            <span className="label">全局</span>
            <span>{stats.global}</span>
          </div>
          <div className="info-row">
            <span className="label">临时</span>
            <span>{stats.temp}</span>
          </div>
          <div className="info-row">
            <span className="label">全局启用</span>
            <span>{stats.global_enabled ? '是' : '否'}</span>
          </div>
        </div>
      </div>

      {stats.by_plugin && Object.keys(stats.by_plugin).length > 0 && (
        <>
          <h2>按插件分布</h2>
          <div className="card">
            <table className="command-table">
              <thead>
                <tr><th>插件</th><th>匹配器数</th></tr>
              </thead>
              <tbody>
                {Object.entries(stats.by_plugin).map(([plugin, count]) => (
                  <tr key={plugin}>
                    <td>{plugin}</td>
                    <td>{count}</td>
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
