import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'
import { Icon } from './Icons.tsx'

export function CommandList() {
  const [commands, setCommands] = useState<api.CommandInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [category, setCategory] = useState('all')

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

  const matched = (cmd: api.CommandInfo) => {
    if (search) {
      const q = search.toLowerCase()
      const hit = cmd.command.toLowerCase().includes(q)
        || (cmd.description || '').toLowerCase().includes(q)
        || (cmd.plugin || '').toLowerCase().includes(q)
        || (cmd.aliases || []).some((a) => a.toLowerCase().includes(q))
      if (!hit) return false
    }
    if (category !== 'all' && (cmd.category || '未分类') !== category) return false
    return true
  }

  const visible = Array.from(categories.entries())
    .map(([cat, cmds]) => [cat, cmds.filter(matched)] as const)
    .filter(([, cmds]) => cmds.length > 0)

  const catOptions = Array.from(categories.keys())

  return (
    <div className="section">
      <div className="section-header">
        <h2>命令列表 <span className="plugin-count">({commands.length})</span></h2>
        <button className="btn-secondary" onClick={fetchData}><Icon name="refresh" size={13} />刷新</button>
      </div>

      <div className="plugin-toolbar">
        <div className="plugin-search">
          <Icon name="search" size={14} />
          <input type="text" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索命令、描述、插件..." spellCheck={false} />
        </div>
        <select className="plugin-sort" value={category} onChange={(e) => setCategory(e.target.value)}>
          <option value="all">全部分类</option>
          {catOptions.map((c) => <option key={c} value={c}>{c}</option>)}
        </select>
        <span className="plugin-count">{visible.reduce((n, [, cmds]) => n + cmds.length, 0)} 个结果</span>
      </div>

      {commands.length === 0 && <p className="empty">没有已注册的命令</p>}
      {visible.length === 0 && commands.length > 0 && <p className="empty">没有匹配的命令</p>}

      {visible.map(([cat, cmds]) => (
        <div key={cat} className="card command-card">
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
                  <td>
                    <code>{cmd.command}</code>
                    {cmd.aliases && cmd.aliases.length > 0 && (
                      <div className="cmd-alias">
                        {cmd.aliases.map((a) => <span key={a} className="chip muted">{a}</span>)}
                      </div>
                    )}
                  </td>
                  <td>
                    {cmd.description || '-'}
                    {cmd.examples && cmd.examples.length > 0 && (
                      <div className="cmd-examples">
                        {cmd.examples.map((ex, i) => <code key={i}>{ex}</code>)}
                      </div>
                    )}
                  </td>
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
