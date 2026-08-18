import { useEffect, useState, useCallback, useRef } from 'react'
import * as api from '../api'
import { useToast } from './Toast.tsx'
import { ConfirmDialog } from './ConfirmDialog.tsx'
import { SkeletonBlock } from './Skeleton.tsx'
import { EmptyState } from './EmptyState.tsx'
import { Icon } from './Icons.tsx'

const REFRESH_INTERVAL = 8000

export function BotStatus() {
  const [bots, setBots] = useState<api.BotInfo[]>([])
  const [health, setHealth] = useState<Record<string, unknown> | null>(null)
  const [stats, setStats] = useState<api.SystemStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [confirmTarget, setConfirmTarget] = useState<{ name: string; action: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)
  const [paused, setPaused] = useState(false)
  const pausedRef = useRef(paused)
  pausedRef.current = paused
  const { toast } = useToast()

  const fetchData = useCallback(async () => {
    try {
      const b = await api.listBots()
      setBots(b)
      setError('')
    } catch (e: unknown) {
      setError((e as Error).message)
    }
    try {
      const h = await api.getHealth()
      setHealth(h)
    } catch { /* ignore health timeout */ }
    try {
      const s = await api.getStats()
      setStats(s)
    } catch { /* ignore stats */ }
    setLoading(false)
  }, [])

  useEffect(() => {
    fetchData()
    const timer = setInterval(() => { if (!pausedRef.current) fetchData() }, REFRESH_INTERVAL)
    return () => clearInterval(timer)
  }, [fetchData])

  const handleAction = useCallback(async (name: string, action: string, fn: (n: string) => Promise<void>) => {
    try {
      await fn(name)
      toast(`${name} ${action}成功`, 'success')
      fetchData()
    } catch (e: unknown) {
      toast(`${action} ${name} 失败: ${(e as Error).message}`, 'error')
    }
  }, [fetchData, toast])

  const handleConfirm = useCallback(async () => {
    if (!confirmTarget) return
    setConfirmLoading(true)
    const { name, action } = confirmTarget
    const fn = action === '停止' ? api.stopBot : api.restartBot
    await handleAction(name, action, fn)
    setConfirmLoading(false)
    setConfirmTarget(null)
  }, [confirmTarget, handleAction])

  const healthStatus = (health?.['status'] as string) || ''
  const healthRoot = health?.['root'] as Record<string, unknown> | undefined
  const healthChildren = (healthRoot?.['children'] as Record<string, unknown>[]) || []

  if (loading) return <div className="section"><h2>Bot 状态</h2><SkeletonBlock /><SkeletonBlock /></div>
  if (error && bots.length === 0) return <div className="error-card">连接失败: {error}</div>

  const running = bots.filter((b) => b.status === 'running').length
  const states = stats?.plugins_by_state ?? {}
  const stateCount = Object.values(states).reduce((a, b) => a + b, 0)
  const goroutines = stats?.goroutine_summary?.total ?? 0

  return (
    <div className="section">
      <div className="section-header">
        <h2><Icon name="grid" size={18} />概览</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <span className="auto-refresh-hint">{paused ? '已暂停' : `每 ${REFRESH_INTERVAL / 1000}s 自动刷新`}</span>
          <button className="btn-secondary" onClick={() => setPaused((v) => !v)}>{paused ? '继续' : '暂停'}</button>
        </div>
      </div>

      {error && <div className="banner error">{error}</div>}

      <div className="stat-grid">
        <div className="stat-card">
          <span className={`stat-icon ${running > 0 ? 'green' : ''}`}><Icon name={running > 0 ? 'play' : 'stop'} size={18} /></span>
          <div className="stat-body">
            <div className="stat-value">{running}/{bots.length}</div>
            <div className="stat-label">运行中的 Bot</div>
          </div>
        </div>
        <div className="stat-card">
          <span className="stat-icon"><Icon name="plugin" size={18} /></span>
          <div className="stat-body">
            <div className="stat-value">{stateCount || bots[0]?.plugin_count || 0}</div>
            <div className="stat-label">已注册插件</div>
          </div>
        </div>
        <div className="stat-card">
          <span className="stat-icon blue"><Icon name="platform" size={18} /></span>
          <div className="stat-body">
            <div className="stat-value">{bots[0]?.platforms?.length ?? 0}</div>
            <div className="stat-label">接入平台</div>
          </div>
        </div>
        <div className="stat-card">
          <span className="stat-icon amber"><Icon name="cpu" size={18} /></span>
          <div className="stat-body">
            <div className="stat-value">{goroutines}</div>
            <div className="stat-label">插件 goroutine</div>
          </div>
        </div>
        <div className="stat-card">
          <span className="stat-icon"><Icon name="box" size={18} /></span>
          <div className="stat-body">
            <div className="stat-value">{bots[0]?.version || '-'}</div>
            <div className="stat-label">框架版本</div>
          </div>
        </div>
        <div className="stat-card">
          <span className="stat-icon green"><Icon name="activity" size={18} /></span>
          <div className="stat-body">
            <div className="stat-value">{bots[0]?.uptime || '-'}</div>
            <div className="stat-label">运行时长</div>
          </div>
        </div>
      </div>

      {bots.length === 0 ? (
        <EmptyState message="没有配置 Bot" hint="请先在配置中添加平台并启动 Bot，或连接到运行中的 Bot 实例。">
          <div style={{ marginTop: '0.75rem' }}>
            <span className="auto-refresh-hint">可在左侧导航中切换到「配置」页</span>
          </div>
        </EmptyState>
      ) : (
        bots.map((bot) => (
          <div key={bot.name} className="card bot-card">
            <div className="card-header">
              <span className={`status-dot ${bot.status}`} />
              <strong>{bot.name}</strong>
              <span className={`state-tag ${bot.status === 'running' ? 'loaded' : 'disabled'}`}>
                {bot.status === 'running' ? '运行中' : '已停止'}
              </span>
            </div>
            <div className="card-body">
              <div className="info-row"><span className="label">版本</span><span>{bot.version}</span></div>
              <div className="info-row"><span className="label">运行时长</span><span>{bot.uptime}</span></div>
              <div className="info-row"><span className="label">平台</span><span>{bot.platforms?.join(', ') || '-'}</span></div>
              <div className="info-row"><span className="label">插件数</span><span>{bot.plugin_count}</span></div>
            </div>
            <div className="card-actions">
              {bot.status === 'running' ? (
                <>
                  <button className="warn" onClick={() => setConfirmTarget({ name: bot.name, action: '停止' })}>
                    <Icon name="stop" size={14} />停止
                  </button>
                  <button onClick={() => setConfirmTarget({ name: bot.name, action: '重启' })}>
                    <Icon name="restart" size={14} />重启
                  </button>
                </>
              ) : (
                <button onClick={() => handleAction(bot.name, '启动', api.startBot)}>
                  <Icon name="play" size={14} />启动
                </button>
              )}
            </div>
          </div>
        ))
      )}

      <ConfirmDialog
        open={!!confirmTarget}
        title={confirmTarget?.action === '停止' ? '停止 Bot' : '重启 Bot'}
        message={`确定要${confirmTarget?.action === '停止' ? '停止' : '重启'} Bot「${confirmTarget?.name}」吗？`}
        confirmLabel={confirmTarget?.action || ''}
        confirmVariant="danger"
        loading={confirmLoading}
        onConfirm={handleConfirm}
        onCancel={() => setConfirmTarget(null)}
      />

      {health && (
        <>
          <div className="section-header" style={{ marginTop: '1.5rem' }}>
            <h2><Icon name="activity" size={18} />健康检查</h2>
            <span className={`health-badge ${healthStatus}`}>{healthStatus}</span>
          </div>
          {healthChildren.length > 0 ? (
            healthChildren.map((child, i) => <HealthNode key={i} node={child} depth={0} />)
          ) : (
            <pre className="health-json">{JSON.stringify(health, null, 2)}</pre>
          )}
        </>
      )}
    </div>
  )
}

function HealthNode({ node, depth }: { node: Record<string, unknown>; depth: number }) {
  const name = node['name'] as string || ''
  const status = node['status'] as string || ''
  const children = (node['children'] as Record<string, unknown>[]) || []
  const kind = node['kind'] as string || ''
  const message = node['message'] as string || ''

  return (
    <div className="health-node" style={{ marginLeft: depth * 1.5 + 'rem' }}>
      <div className="health-node-header">
        <span className={`status-dot ${status === 'healthy' || status === 'running' ? 'running' : status === 'unhealthy' || status === 'critical' || status === 'error' ? 'error' : 'stopped'}`} />
        <span className="health-node-name">{name || kind}</span>
        <span className={`health-badge ${status}`}>{status}</span>
      </div>
      {message && <div className="health-node-msg">{message}</div>}
      {children.map((child, i) => <HealthNode key={i} node={child} depth={depth + 1} />)}
    </div>
  )
}
