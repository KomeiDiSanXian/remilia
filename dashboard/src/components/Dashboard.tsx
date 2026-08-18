import { useEffect, useState, useCallback } from 'react'
import { BotStatus } from './BotStatus.tsx'
import { PluginList } from './PluginList.tsx'
import { PlatformList } from './PlatformList.tsx'
import { CommandList } from './CommandList.tsx'
import { MatcherView } from './MatcherView.tsx'
import { AuditLogView } from './AuditLogView.tsx'
import { PermissionView } from './PermissionView.tsx'
import { FSMView } from './FSMView.tsx'
import { SchedulerView } from './SchedulerView.tsx'
import { ConfigEditor } from './ConfigEditor.tsx'
import { LogViewer } from './LogViewer.tsx'
import { ToastProvider } from './Toast.tsx'
import { Icon, type IconName } from './Icons.tsx'
import * as api from '../api'

interface DashboardProps {
  onLogout: () => void
}

type Tab =
  | 'overview' | 'plugins' | 'platforms' | 'commands' | 'matchers'
  | 'auditlog' | 'permission' | 'fsm' | 'scheduler' | 'config' | 'logs'

interface TabDef {
  key: Tab
  label: string
  icon: IconName
  group: '运营' | '引擎' | '管理'
}

type TabGroup = '运营' | '引擎' | '管理'

const TABS: TabDef[] = [
  { key: 'overview', label: '概览', icon: 'grid', group: '运营' },
  { key: 'plugins', label: '插件', icon: 'plugin', group: '运营' },
  { key: 'platforms', label: '平台', icon: 'platform', group: '运营' },
  { key: 'commands', label: '命令', icon: 'command', group: '引擎' },
  { key: 'matchers', label: '匹配器', icon: 'matcher', group: '引擎' },
  { key: 'fsm', label: '状态机', icon: 'fsm', group: '引擎' },
  { key: 'scheduler', label: '调度器', icon: 'clock', group: '引擎' },
  { key: 'auditlog', label: '审计日志', icon: 'audit', group: '管理' },
  { key: 'permission', label: '权限', icon: 'shield', group: '管理' },
  { key: 'config', label: '配置', icon: 'config', group: '管理' },
  { key: 'logs', label: '日志', icon: 'logs', group: '管理' },
]

const TAB_KEYS = TABS.map((t) => t.key)
const TAB_GROUPS: TabGroup[] = ['运营', '引擎', '管理']

export function Dashboard({ onLogout }: DashboardProps) {
  const [tab, setTab] = useState<Tab>('overview')
  const [version, setVersion] = useState<api.VersionInfo | null>(null)
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [connected, setConnected] = useState(true)
  const [badges, setBadges] = useState<{ plugins?: string; platforms?: string; pluginErrors?: boolean }>({})

  const checkConnection = useCallback(() => {
    api.getVersion().then((v) => { setVersion(v); setConnected(true) }).catch(() => setConnected(false))
  }, [])

  const fetchBadges = useCallback(async () => {
    try {
      const [plugins, platforms] = await Promise.all([api.listPlugins(), api.listPlatforms()])
      const errorCount = plugins.filter((p) => p.state === 'Error' || !!p.last_error).length
      const running = platforms.filter((p) => p.running).length
      setBadges({
        plugins: String(plugins.length),
        platforms: `${running}/${platforms.length}`,
        pluginErrors: errorCount > 0,
      })
    } catch { /* 忽略：连接状态由 checkConnection 负责 */ }
  }, [])

  useEffect(() => {
    checkConnection()
    const timer = setInterval(checkConnection, 15000)
    return () => clearInterval(timer)
  }, [checkConnection])

  useEffect(() => {
    fetchBadges()
    const timer = setInterval(fetchBadges, 30000)
    return () => clearInterval(timer)
  }, [fetchBadges])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        document.dispatchEvent(new CustomEvent('key-escape'))
        return
      }
      if (e.ctrlKey || e.metaKey) {
        if (e.key === 's') {
          e.preventDefault()
          document.dispatchEvent(new CustomEvent('key-save'))
          return
        }
        const num = parseInt(e.key)
        if (num >= 1 && num <= TAB_KEYS.length) {
          setTab(TAB_KEYS[num - 1])
          setMobileOpen(false)
        }
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [])

  // 关闭移动端抽屉
  useEffect(() => {
    const close = () => setMobileOpen(false)
    window.addEventListener('resize', close)
    return () => window.removeEventListener('resize', close)
  }, [])

  const handleLogout = useCallback(() => onLogout(), [onLogout])
  const activeTab = TABS.find((t) => t.key === tab)!

  const renderBadge = (t: TabDef) => {
    if (t.key === 'plugins' && badges.plugins !== undefined) {
      return <span className={`sidebar-badge ${badges.pluginErrors ? 'error' : ''}`}>{badges.plugins}</span>
    }
    if (t.key === 'platforms' && badges.platforms !== undefined) {
      return <span className="sidebar-badge">{badges.platforms}</span>
    }
    return null
  }

  return (
    <div className="dashboard">
      {mobileOpen && <div className="sidebar-backdrop" onClick={() => setMobileOpen(false)} />}
      <aside className={`sidebar ${sidebarOpen ? '' : 'sidebar-collapsed'} ${mobileOpen ? 'mobile-open' : ''}`}>
        <div className="sidebar-header">
          <button
            className="sidebar-toggle"
            onClick={() => setSidebarOpen((v) => !v)}
            title="切换侧边栏"
          >
            <Icon name={sidebarOpen ? 'chevronLeft' : 'chevronRight'} size={12} />
          </button>
          <div className="sidebar-brand">
            <span className="sidebar-logo">R</span>
            <h1>Remilia</h1>
            {version && <span className="sidebar-version">v{version.version}</span>}
          </div>
        </div>

        <nav className="sidebar-nav">
          {TAB_GROUPS.map((group) => (
            <div key={group}>
              <div className="sidebar-group-label">{group}</div>
              {TABS.filter((t) => t.group === group).map((t) => (
                <button
                  key={t.key}
                  className={`sidebar-item ${tab === t.key ? 'active' : ''}`}
                  onClick={() => { setTab(t.key); setMobileOpen(false) }}
                  title={`${t.label} (Ctrl+${TAB_KEYS.indexOf(t.key) + 1})`}
                >
                  <Icon name={t.icon} size={17} className="sidebar-icon" />
                  <span className="sidebar-label">{t.label}</span>
                  {renderBadge(t)}
                </button>
              ))}
            </div>
          ))}
        </nav>

        <div className="sidebar-footer">
          <button className="sidebar-item logout" onClick={handleLogout} title="断开连接">
            <Icon name="logout" size={17} className="sidebar-icon" />
            <span className="sidebar-label">断开连接</span>
          </button>
        </div>
      </aside>

      <div className="dashboard-main">
        <div className="dashboard-topbar">
          <div className="topbar-left">
            <button className="btn-icon topbar-menu-btn" onClick={() => setMobileOpen(true)} title="打开菜单">
              <Icon name="menu" size={18} />
            </button>
            <span className={`connection-dot ${connected ? 'connected' : 'disconnected'}`} title={connected ? '已连接' : '连接断开'} />
            <span className="topbar-title">{activeTab.label}</span>
          </div>
          <div className="topbar-right">
            <span className={`conn-pill ${connected ? 'online' : 'offline'}`}>
              <Icon name={connected ? 'check' : 'alert'} size={12} />
              <span className="pill-text">{connected ? '已连接' : '连接断开'}</span>
            </span>
            <button className="btn-icon" onClick={checkConnection} title="刷新连接状态">
              <Icon name="refresh" size={16} />
            </button>
            <button className="btn-secondary topbar-logout" onClick={handleLogout}>断开</button>
          </div>
        </div>

        <main>
          <ToastProvider>
            {tab === 'overview' && <BotStatus />}
            {tab === 'plugins' && <PluginList />}
            {tab === 'platforms' && <PlatformList />}
            {tab === 'commands' && <CommandList />}
            {tab === 'matchers' && <MatcherView />}
            {tab === 'auditlog' && <AuditLogView />}
            {tab === 'permission' && <PermissionView />}
            {tab === 'fsm' && <FSMView />}
            {tab === 'scheduler' && <SchedulerView />}
            {tab === 'config' && <ConfigEditor />}
            {tab === 'logs' && <LogViewer />}
          </ToastProvider>
        </main>
      </div>
    </div>
  )
}
