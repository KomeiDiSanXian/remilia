import { useEffect, useState, useCallback, useRef } from 'react'
import * as api from '../api'
import { useToast } from './Toast.tsx'
import { ConfirmDialog } from './ConfirmDialog.tsx'
import { SkeletonGrid } from './Skeleton.tsx'
import { EmptyState } from './EmptyState.tsx'
import { Icon } from './Icons.tsx'

type SortKey = 'name' | 'name_desc' | 'state' | 'matchers'
type StateFilter = 'all' | 'Loaded' | 'Disabled' | 'Error' | 'Other'

const stateLabels: Record<string, string> = {
  Loaded: '已加载', Disabled: '已禁用', Error: '错误',
  Loading: '加载中', Unloaded: '未加载', Unloading: '卸载中',
}

const FILTERS: { key: StateFilter; label: string }[] = [
  { key: 'all', label: '全部' },
  { key: 'Loaded', label: '已加载' },
  { key: 'Disabled', label: '已禁用' },
  { key: 'Error', label: '错误' },
  { key: 'Other', label: '其他' },
]

export function PluginList() {
  const [plugins, setPlugins] = useState<api.PluginInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [detailTarget, setDetailTarget] = useState<string | null>(null)
  const [detailData, setDetailData] = useState<api.PluginDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [search, setSearch] = useState('')
  const [sortKey, setSortKey] = useState<SortKey>('name')
  const [stateFilter, setStateFilter] = useState<StateFilter>('all')
  const [confirmTarget, setConfirmTarget] = useState<{ name: string; action: 'disable' | 'reload' } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)
  const [paused, setPaused] = useState(false)
  const pausedRef = useRef(paused)
  pausedRef.current = paused
  const { toast } = useToast()

  const filtered = plugins
    .filter((p) => {
      if (search && !p.name.toLowerCase().includes(search.toLowerCase())) return false
      if (stateFilter === 'all') return true
      if (stateFilter === 'Other') return !['Loaded', 'Disabled', 'Error'].includes(p.state)
      return p.state === stateFilter
    })
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
  const countBy = (f: StateFilter) => {
    if (f === 'all') return plugins.length
    if (f === 'Other') return plugins.filter((p) => !['Loaded', 'Disabled', 'Error'].includes(p.state)).length
    return plugins.filter((p) => p.state === f).length
  }

  return (
    <div className="section">
      <div className="section-header">
        <h2>插件管理 <span className="plugin-count">({plugins.length})</span></h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <span className="auto-refresh-hint">{paused ? '已暂停' : '每 10s 自动刷新'}</span>
          <button className="btn-secondary" onClick={() => setPaused((v) => !v)}>{paused ? '继续' : '暂停'}</button>
          <button className="btn-secondary" onClick={fetchData}><Icon name="refresh" size={13} />刷新</button>
        </div>
      </div>

      <div className="plugin-toolbar">
        <div className="plugin-search">
          <Icon name="search" size={14} />
          <input type="text" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索插件..." spellCheck={false} />
        </div>
        <div className="filter-chips">
          {FILTERS.map((f) => (
            <button
              key={f.key}
              className={`filter-chip ${stateFilter === f.key ? 'active' : ''}`}
              onClick={() => setStateFilter(f.key)}
            >
              {f.label}<span className="count">{countBy(f.key)}</span>
            </button>
          ))}
        </div>
        <select className="plugin-sort" value={sortKey} onChange={(e) => setSortKey(e.target.value as SortKey)}>
          <option value="name">名称 A-Z</option>
          <option value="name_desc">名称 Z-A</option>
          <option value="state">按状态</option>
          <option value="matchers">匹配器数</option>
        </select>
      </div>

      {filtered.length === 0 && <EmptyState message="没有匹配的插件" hint="调整搜索关键词或状态筛选条件。" />}

      <div className="plugin-grid">
        {filtered.map((p) => (
          <div key={p.name} className={`card plugin-card ${p.state === 'Error' || p.last_error ? 'state-error' : ''}`}>
            <div className="card-header">
              <span className={`status-dot ${p.state === 'Loaded' ? 'running' : p.state === 'Error' ? 'error' : 'stopped'}`} />
              <strong>{p.name}</strong>
              <span className={`state-tag ${p.state}`}>{stateLabels[p.state] || p.state}</span>
            </div>
            <div className="plugin-meta-row">
              {p.version && <span className="chip muted">{p.version}</span>}
              <span className="chip info"><Icon name="matcher" size={11} />{p.matcher_count}</span>
              {p.uptime && <span className="chip muted"><Icon name="clock" size={11} />{p.uptime}</span>}
            </div>
            {p.last_error && (
              <div className="plugin-error">
                <Icon name="alert" size={12} /> {p.last_error}
              </div>
            )}
            <div className="card-body">
              {p.dependencies && p.dependencies.length > 0 && (
                <div className="info-row"><span className="label">依赖</span><span>{p.dependencies.join(', ')}</span></div>
              )}
            </div>
            <div className="card-actions">
              {p.state === 'Disabled' ? (
                <button onClick={() => doAction(p.name, 'enable')}><Icon name="play" size={13} />启用</button>
              ) : (
                <button className="warn" onClick={() => setConfirmTarget({ name: p.name, action: 'disable' })}>禁用</button>
              )}
              <button onClick={() => setConfirmTarget({ name: p.name, action: 'reload' })}><Icon name="restart" size={13} />重载</button>
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
            <div className="dialog-header">
              {detailTarget}
              {detailData && <span className={`dialog-header-tag ${detailData.State === 'Loaded' ? 'running' : 'stopped'}`}>{stateLabels[detailData.State] || detailData.State}</span>}
            </div>
            {detailLoading && <div className="dialog-body">加载中...</div>}
            {detailData && (
              <div className="dialog-body plugin-detail-body">
                <div className="info-row"><span className="label">状态</span><span>{stateLabels[detailData.State] || detailData.State}</span></div>
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
                    {meta.Homepage && <div className="info-row"><span className="label">主页</span><a href={meta.Homepage} target="_blank" rel="noopener noreferrer">{meta.Homepage}</a></div>}
                    {meta.Repository && <div className="info-row"><span className="label">仓库</span><a href={meta.Repository} target="_blank" rel="noopener noreferrer">{meta.Repository}</a></div>}
                    {meta.Tags && meta.Tags.length > 0 && <div className="info-row"><span className="label">标签</span><span>{meta.Tags.join(', ')}</span></div>}
                    {meta.Dependencies && meta.Dependencies.length > 0 && <div className="info-row"><span className="label">依赖</span><span>{meta.Dependencies.join(', ')}</span></div>}
                  </>
                )}
              </div>
            )}
            <div className="dialog-actions">
              <button className="btn-secondary" onClick={() => { setDetailTarget(null); setDetailData(null) }}>关闭</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
