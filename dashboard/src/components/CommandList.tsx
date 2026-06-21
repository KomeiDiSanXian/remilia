import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'

export function CommandList() {
  const [commands, setCommands] = useState<api.CommandInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchData = useCallback(async () => {
    try {
      const cmds = await api.listCommands()
      setCommands(cmds)
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

  // 按分类分组
  const categories = new Map<string, api.CommandInfo[]>()
  for (const cmd of commands) {
    const cat = cmd.category || '未分类'
    if (!categories.has(cat)) categories.set(cat, [])
    categories.get(cat)!.push(cmd)
  }

  if (commands.length === 0) return <p className="empty">没有已注册的命令</p>

  return (
    <div className="section">
      <h2>命令列表（{commands.length}）</h2>
      {Array.from(categories.entries()).map(([cat, cmds]) => (
        <div key={cat} className="card">
          <div className="card-header">
            <strong>{cat}</strong>
            <span className="tag">{cmds.length} 个命令</span>
          </div>
          <table className="command-table">
            <thead>
              <tr>
                <th>命令</th>
                <th>描述</th>
                <th>用法</th>
                <th>插件</th>
              </tr>
            </thead>
            <tbody>
              {cmds.map((cmd) => (
                <tr key={cmd.command}>
                  <td><code>{cmd.command}</code></td>
                  <td>{cmd.description || '-'}</td>
                  <td><code>{cmd.usage || '-'}</code></td>
                  <td>{cmd.plugin || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ))}
    </div>
  )
}
