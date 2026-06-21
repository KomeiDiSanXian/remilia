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
import * as api from '../api'

interface DashboardProps {
  onLogout: () => void
}

type Tab = 'overview' | 'plugins' | 'platforms' | 'commands' | 'matchers' | 'auditlog' | 'permission' | 'fsm' | 'scheduler' | 'config' | 'logs'

const TABS: { key: Tab; label: string; icon: string }[] = [
  { key: 'overview', label: '概览', icon: '◉' },
  { key: 'plugins', label: '插件', icon: '▤' },
  { key: 'platforms', label: '平台', icon: '◐' },
  { key: 'commands', label: '命令', icon: '⌘' },
  { key: 'matchers', label: '匹配器', icon: '⚙' },
  { key: 'auditlog', label: '审计日志', icon: '☰' },
  { key: 'permission', label: '权限', icon: '⚡' },
  { key: 'fsm', label: '状态机', icon: '↻' },
  { key: 'scheduler', label: '调度器', icon: '⏰' },
  { key: 'config', label: '配置', icon: '⚙' },
  { key: 'logs', label: '日志', icon: '≡' },
]

const TAB_KEYS = TABS.map((t) => t.key)

export function Dashboard({ onLogout }: DashboardProps) {
  const [tab, setTab] = useState<Tab>('overview')
  const [version, setVersion] = useState<api.VersionInfo | null>(null)
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [connected, setConnected] = useState(true)

  useEffect(() => {
    api.getVersion().then((v) => { setVersion(v); setConnected(true) }).catch(() => setConnected(false))
    const timer = setInterval(() => {
      api.getVersion().then(() => setConnected(true)).catch(() => setConnected(false))
    }, 15000)
    return () => clearInterval(timer)
  }, [])

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        const esc = new CustomEvent('key-escape')
        document.dispatchEvent(esc)
        return
      }
      if (e.ctrlKey || e.metaKey) {
        if (e.key === 's') {
          e.preventDefault()
          const save = new CustomEvent('key-save')
          document.dispatchEvent(save)
          return
        }
        const num = parseInt(e.key)
        if (num >= 1 && num <= 9) {
          setTab(TAB_KEYS[num - 1])
        }
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [])

  const handleLogout = useCallback(() => onLogout(), [onLogout])

  return (
    <div className="dashboard">
      <aside className={`sidebar ${sidebarOpen ? '' : 'sidebar-collapsed'}`}>
        <div className="sidebar-header">
          <button className="sidebar-toggle" onClick={() => setSidebarOpen((v) => !v)} title="切换侧边栏">
            {sidebarOpen ? '◀' : '▶'}
          </button>
          <h1>Remilia</h1>
          {version && <span className="sidebar-version">v{version.version}</span>}
        </div>
        <nav className="sidebar-nav">
          {TABS.map((t, i) => (
            <button key={t.key} className={`sidebar-item ${tab === t.key ? 'active' : ''}`} onClick={() => setTab(t.key)} title={`${t.label} (Ctrl+${i + 1})`}>
              <span className="sidebar-icon">{t.icon}</span>
              <span className="sidebar-label">{t.label}</span>
            </button>
          ))}
        </nav>
      </aside>

      <div className="dashboard-main">
        <div className="dashboard-topbar">
          <div className="topbar-left">
            <span className={`connection-dot ${connected ? 'connected' : 'disconnected'}`} title={connected ? '已连接' : '连接断开'} />
          </div>
          <button className="logout topbar-logout" onClick={handleLogout} title="断开连接">断开</button>
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
