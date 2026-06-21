import { useEffect, useState, useCallback, useRef } from 'react'
import * as api from '../api'
import { useToast } from './Toast.tsx'
import { ConfirmDialog } from './ConfirmDialog.tsx'
import { SkeletonGrid } from './Skeleton.tsx'
import { EmptyState } from './EmptyState.tsx'

type SortKey = 'name' | 'name_desc' | 'state' | 'matchers'

const stateLabels: Record<string, string> = {
  Loaded: '已加载', Disabled: '已禁用', Error: '错误',
  Loading: '加载中', Unloaded: '未加载', Unloading: '卸载中',
}

export function PluginList() {
  const [plugins, setPlugins] = useState<api.PluginInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [detailTarget, setDetailTarget] = useState<string | null>(null)
  const [detailData, setDetailData] = useState<api.PluginDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [search, setSearch] = useState('')
  const [sortKey, setSortKey] = useState<SortKey>('name')
  const [confirmTarget, setConfirmTarget] = useState<{ name: string; action: 'disable' | 'reload' } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)
  const [paused, setPaused] = useState(false)
  const pausedRef = useRef(paused)
  pausedRef.current = paused
  const { toast } = useToast()

  const filtered = plugins
    .filter((p) => !search || p.name.toLowerCase().includes(search.toLowerCase()))
    .sort((a, b) => {
      switch (sortKey) {
        case 'name_desc': return b.name.localeCompare(a.name)
        case 'state': return a.state.localeCompare(b.state) || a.name.localeCompare(b.name)
        case 'matchers': return b.matcher_count - a.matcher_count || a.name.localeCompare(b.name)
        default: return a.name.localeCompare(b.name)
      }
    })

  const fetchData = useCallback(async () => {
    try {
      const p = await api.listPlugins()
      setPlugins(p)
      setError('')
    } catch (e: unknown) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchData()
    const timer = setInterval(() => { if (!pausedRef.current) fetchData() }, 10000)
    return () => clearInterval(timer)
  }, [fetchData])

  const openDetail = useCallback(async (name: string) => {
    setDetailTarget(name)
    setDetailLoading(true)
    try {
      setDetailData(await api.getPluginDetail(name))
    } catch { setDetailData(null) }
    finally { setDetailLoading(false) }
  }, [])

  const doAction = useCallback(async (name: string, action: 'enable' | 'disable' | 'reload') => {
    try {
      if (action === 'enable') await api.enablePlugin(name)
      else if (action === 'disable') await api.disablePlugin(name)
      else await api.reloadPlugin(name)
      toast(`${name}: ${action === 'enable' ? '已启用' : action === 'disable' ? '已禁用' : '已重载'}`, 'success')
      fetchData()
    } catch (e: unknown) {
      toast(`${name} ${action} 失败: ${(e as Error).message}`, 'error')
    }
  }, [fetchData, toast])

  const handleConfirm = useCallback(async () => {
    if (!confirmTarget) return
    setConfirmLoading(true)
    await doAction(confirmTarget.name, confirmTarget.action)
    setConfirmLoading(false)
    setConfirmTarget(null)
  }, [confirmTarget, doAction])

  if (loading) return <div className="section"><h2>插件管理</h2><SkeletonGrid count={6} /></div>
  if (error) return <div className="error-card">获取失败: {error}</div>

  const meta = detailData?.Metadata

  return (
    <div className="section">
      <div className="section-header">
        <h2>插件管理 ({plugins.length})</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <span className="auto-refresh-hint">{paused ? '已暂停' : '每 10s 自动刷新'}</span>
          <button className="btn-secondary" onClick={() => setPaused((v) => !v)}>{paused ? '继续' : '暂停'}</button>
          <button className="refresh" onClick={fetchData}>刷新</button>
        </div>
      </div>

      <div className="plugin-toolbar">
        <div className="plugin-search">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/></svg>
          <input type="text" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索插件..." spellCheck={false} />
        </div>
        <select className="plugin-sort" value={sortKey} onChange={(e) => setSortKey(e.target.value as SortKey)}>
          <option value="name">名称 A-Z</option>
          <option value="name_desc">名称 Z-A</option>
          <option value="state">按状态</option>
          <option value="matchers">匹配器数</option>
        </select>
        <span className="plugin-count">{filtered.length} / {plugins.length}</span>
      </div>

      {filtered.length === 0 && (plugins.length === 0
        ? <EmptyState message="没有注册插件" hint="插件在 Bot 启动时自动注册。请确认 Bot 正在运行。" />
        : <p className="empty">没有匹配的插件</p>
      )}

      <div className="plugin-grid">
        {filtered.map((p) => (
          <div key={p.name} className={`card plugin-card state-${p.state.toLowerCase()}`}>
            <div className="card-header">
              <span className={`state-tag ${p.state.toLowerCase()}`}>{stateLabels[p.state] || p.state}</span>
              <strong>{p.name}</strong>
              <span className="version">{p.version || '-'}</span>
            </div>
            <div className="card-body">
              <div className="info-row"><span className="label">匹配器</span><span>{p.matcher_count}</span></div>
              <div className="info-row"><span className="label">运行时长</span><span>{p.uptime || '-'}</span></div>
              {p.dependencies && p.dependencies.length > 0 && (
                <div className="info-row"><span className="label">依赖</span><span>{p.dependencies.join(', ')}</span></div>
              )}
              {p.last_error && <div className="info-row error-text"><span className="label">错误</span><span>{p.last_error}</span></div>}
            </div>
            <div className="card-actions">
              {p.state === 'Disabled' ? (
                <button onClick={() => doAction(p.name, 'enable')}>启用</button>
              ) : (
                <button className="warn" onClick={() => setConfirmTarget({ name: p.name, action: 'disable' })}>禁用</button>
              )}
              <button onClick={() => setConfirmTarget({ name: p.name, action: 'reload' })}>重载</button>
              <button className="btn-secondary" onClick={() => openDetail(p.name)}>详情</button>
            </div>
          </div>
        ))}
      </div>

      <ConfirmDialog
        open={!!confirmTarget}
        title={confirmTarget?.action === 'disable' ? '禁用插件' : '重载插件'}
        message={`确定要${confirmTarget?.action === 'disable' ? '禁用' : '重载'}插件「${confirmTarget?.name}」吗？`}
        confirmLabel={confirmTarget?.action === 'disable' ? '禁用' : '重载'}
        confirmVariant={confirmTarget?.action === 'disable' ? 'danger' : 'primary'}
        loading={confirmLoading}
        onConfirm={handleConfirm}
        onCancel={() => setConfirmTarget(null)}
      />

      {/* --- Detail Modal --- */}
      {detailTarget && (
        <div className="dialog-overlay" onClick={() => { setDetailTarget(null); setDetailData(null) }}>
          <div className="dialog plugin-detail-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="dialog-header">{detailTarget}</div>
            {detailLoading && <div className="dialog-body">加载中...</div>}
            {detailData && (
              <div className="dialog-body plugin-detail-body">
                <div className="info-row"><span className="label">状态</span><span>{detailData.State}</span></div>
                <div className="info-row"><span className="label">匹配器</span><span>{detailData.MatcherCount}</span></div>
                <div className="info-row"><span className="label">运行时长</span><span>{Math.floor(detailData.Uptime / 1e9)}s</span></div>
                <div className="info-row"><span className="label">Goroutines</span><span>{detailData.GoroutineCount}</span></div>
                <div className="info-row"><span className="label">EventBus 订阅</span><span>{detailData.EventBusSubscriptions}</span></div>
                <div className="info-row"><span className="label">状态持久化</span><span>{detailData.HasSaveState ? '是' : '否'}</span></div>
                {detailData.LastError && <div className="info-row error-text"><span className="label">错误</span><span>{detailData.LastError}</span></div>}
                {meta && (
                  <>
                    <div className="plugin-detail-divider">元数据</div>
                    <div className="info-row"><span className="label">版本</span><span>{meta.Version}</span></div>
                    {meta.Author && <div className="info-row"><span className="label">作者</span><span>{meta.Author}</span></div>}
                    {meta.Description && <div className="info-row"><span className="label">描述</span><span>{meta.Description}</span></div>}
                    {meta.Category && <div className="info-row"><span className="label">分类</span><span>{meta.Category}</span></div>}
                    {meta.Homepage && <div className="info-row"><span className="label">主页</span><a href={meta.Homepage} rel="noopener noreferrer">{meta.Homepage}</a></div>}
                    {meta.Repository && <div className="info-row"><span className="label">仓库</span><a href={meta.Repository} rel="noopener noreferrer">{meta.Repository}</a></div>}
                    {meta.Tags && meta.Tags.length > 0 && <div className="info-row"><span className="label">标签</span><span>{meta.Tags.join(', ')}</span></div>}
                    {meta.Dependencies && meta.Dependencies.length > 0 && <div className="info-row"><span className="label">依赖</span><span>{meta.Dependencies.join(', ')}</span></div>}
                  </>
                )}
              </div>
            )}
            <div className="dialog-actions"><button onClick={() => { setDetailTarget(null); setDetailData(null) }}>关闭</button></div>
          </div>
        </div>
      )}
    </div>
  )
}
