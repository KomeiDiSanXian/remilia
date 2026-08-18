import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'
import { useToast } from './Toast.tsx'
import { ConfirmDialog } from './ConfirmDialog.tsx'
import { SkeletonBlock } from './Skeleton.tsx'
import { EmptyState } from './EmptyState.tsx'
import { Icon } from './Icons.tsx'

const PLATFORM_TYPES = [
  { type: 'qq', label: 'QQ', fields: ['app_id', 'bot_id', 'token', 'secret'] },
  { type: 'onebot', label: 'OneBot', fields: ['url', 'listen_addr', 'token', 'secret'] },
  { type: 'discord', label: 'Discord', fields: ['token'] },
  { type: 'satori', label: 'Satori', fields: ['server_url', 'token', 'platform', 'user_id'] },
  { type: 'milky', label: 'Milky', fields: ['base_url', 'access_token'] },
  { type: 'telegram', label: 'Telegram', fields: ['token'] },
]

interface AddFormData {
  type: string
  [key: string]: unknown
}

export function PlatformList() {
  const [platforms, setPlatforms] = useState<api.PlatformInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [detailTarget, setDetailTarget] = useState<api.PlatformInfo | null>(null)
  const [showAdd, setShowAdd] = useState(false)
  const [addForm, setAddForm] = useState<AddFormData>({ type: 'qq' })
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState('')
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const { toast } = useToast()

  const fetchData = useCallback(async () => {
    try {
      const p = await api.listPlatforms()
      setPlatforms(p)
      setError('')
    } catch (e: unknown) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const handleAdd = useCallback(async () => {
    setAdding(true)
    setAddError('')
    try {
      const config: Record<string, unknown> = {}
      for (const [k, v] of Object.entries(addForm)) {
        if (k !== 'type') config[k] = v
      }
      await api.addPlatform({ type: addForm.type as string, config })
      setShowAdd(false)
      setAddForm({ type: 'qq' })
      toast('平台配置已添加，重启 bot 后生效', 'success')
    } catch (e: unknown) {
      setAddError((e as Error).message)
    } finally {
      setAdding(false)
    }
  }, [addForm, toast])

  const handleDelete = useCallback(async (name: string) => {
    setDeleting(true)
    try {
      await api.deletePlatform(name)
      setPlatforms((prev) => prev.filter((p) => p.name !== name))
      setDeleteConfirm(null)
      toast('平台已移除', 'success')
    } catch (e: unknown) {
      setError((e as Error).message)
    } finally {
      setDeleting(false)
    }
  }, [toast])

  if (loading) return <div className="section"><h2>平台适配器</h2><SkeletonBlock /><SkeletonBlock /></div>
  if (error) return <div className="error-card">获取失败: {error}</div>

  const selectedType = PLATFORM_TYPES.find((t) => t.type === addForm.type)
  const runningCount = platforms.filter((p) => p.running).length

  return (
    <div className="section">
      <div className="section-header">
        <h2>平台适配器 <span className="plugin-count">({runningCount}/{platforms.length} 运行中)</span></h2>
        <div className="config-actions">
          <button onClick={() => setShowAdd(true)}><Icon name="plus" size={14} />添加平台</button>
          <button className="btn-secondary" onClick={fetchData}><Icon name="refresh" size={13} />刷新</button>
        </div>
      </div>

      {platforms.length === 0 && (
        <EmptyState
          message="没有已注册的平台"
          hint="点击「添加平台」按钮添加一个新的聊天平台适配器。"
          action={{ label: '添加平台', onClick: () => setShowAdd(true) }}
        />
      )}

      <div className="platform-grid">
        {platforms.map((p) => (
          <div key={p.name} className="card platform-card clickable" onClick={() => setDetailTarget(p)}>
            <div className="card-header">
              <span className={`status-dot ${p.running ? 'running' : 'stopped'}`} />
              <strong>{p.name}</strong>
              <span className={`state-tag ${p.running ? 'loaded' : 'disabled'}`}>{p.running ? '运行中' : '已停止'}</span>
            </div>
            <div className="card-body">
              {p.bot_id && (
                <div className="info-row"><span className="label">Bot ID</span><span className="mono">{p.bot_id}</span></div>
              )}
              {p.capabilities && (
                <div className="info-row" style={{ alignItems: 'flex-start' }}>
                  <span className="label">能力集</span>
                  <div className="capability-list">
                    {Object.keys(p.capabilities).filter((k) => p.capabilities![k]).map((k) => (
                      <span key={k} className="chip info">{k}</span>
                    ))}
                  </div>
                </div>
              )}
            </div>
            <div className="card-actions">
              <button className="btn-secondary" onClick={(e) => { e.stopPropagation(); setDetailTarget(p) }}>详情</button>
            </div>
          </div>
        ))}
      </div>

      {/* --- Detail Modal --- */}
      {detailTarget && (
        <div className="dialog-overlay" onClick={() => setDetailTarget(null)}>
          <div className="dialog" onClick={(e) => e.stopPropagation()}>
            <div className="dialog-header">
              {detailTarget.name}
              <span className={`dialog-header-tag ${detailTarget.running ? 'running' : 'stopped'}`}>
                {detailTarget.running ? '运行中' : '已停止'}
              </span>
            </div>
            <div className="dialog-body">
              <div className="info-row"><span className="label">名称</span><span>{detailTarget.name}</span></div>
              <div className="info-row"><span className="label">状态</span><span>{detailTarget.running ? '运行中' : '已停止'}</span></div>
              {detailTarget.bot_id && <div className="info-row"><span className="label">Bot ID</span><span className="mono">{detailTarget.bot_id}</span></div>}
              {detailTarget.bot_name && <div className="info-row"><span className="label">Bot 名称</span><span>{detailTarget.bot_name}</span></div>}
              {detailTarget.capabilities && (
                <div className="info-row" style={{ alignItems: 'flex-start' }}>
                  <span className="label">能力集</span>
                  <div className="capability-list">
                    {Object.keys(detailTarget.capabilities).filter((k) => detailTarget.capabilities![k]).map((k) => (
                      <span key={k} className="chip info">{k}</span>
                    ))}
                  </div>
                </div>
              )}
            </div>
            <div className="dialog-actions">
              <button className="warn" onClick={() => { setDeleteConfirm(detailTarget.name); setDetailTarget(null) }} disabled={deleting}>
                <Icon name="trash" size={13} />删除此平台
              </button>
              <button className="btn-secondary" onClick={() => setDetailTarget(null)}>关闭</button>
            </div>
          </div>
        </div>
      )}

      {/* --- Add Modal --- */}
      {showAdd && (
        <div className="dialog-overlay" onClick={() => setShowAdd(false)}>
          <div className="dialog add-platform-dialog" onClick={(e) => e.stopPropagation()}>
            <div className="dialog-header">添加平台</div>
            <div className="add-platform-form">
              <div className="field">
                <label>平台类型</label>
                <select value={addForm.type as string} onChange={(e) => setAddForm({ type: e.target.value })}>
                  {PLATFORM_TYPES.map((t) => <option key={t.type} value={t.type}>{t.label}</option>)}
                </select>
              </div>
              {selectedType?.fields.map((field) => (
                <div className="field" key={field}>
                  <label>{field}</label>
                  <input type="text" value={(addForm[field] as string) || ''} onChange={(e) => setAddForm((prev) => ({ ...prev, [field]: e.target.value }))} placeholder={field} />
                </div>
              ))}
              {addError && <div className="error">{addError}</div>}
            </div>
            <div className="dialog-actions">
              <button className="btn-secondary" onClick={() => setShowAdd(false)} disabled={adding}>取消</button>
              <button onClick={handleAdd} disabled={adding}>{adding ? '添加中...' : '添加'}</button>
            </div>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={!!deleteConfirm}
        title="确认删除"
        message={`确定要删除平台「${deleteConfirm}」吗？\n此操作会立即停止适配器并从配置中移除。`}
        confirmLabel="删除"
        confirmVariant="danger"
        loading={deleting}
        onConfirm={() => deleteConfirm && handleDelete(deleteConfirm)}
        onCancel={() => setDeleteConfirm(null)}
      />
    </div>
  )
}
