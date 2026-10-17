import { Fragment, useEffect, useState, useCallback } from 'react'
import * as api from '../api'
import { useToast } from './Toast.tsx'
import { ConfirmDialog } from './ConfirmDialog.tsx'
import { Icon } from './Icons.tsx'

// 活动状态：这些任务可以取消。
const ACTIVE_STATUSES = new Set([
  'pending', 'planning', 'ready', 'running',
  'waiting_approval', 'waiting_input', 'paused', 'interrupted',
])

const STATUS_TEXT: Record<string, string> = {
  pending: '待执行',
  planning: '规划中',
  ready: '就绪',
  running: '进行中',
  waiting_approval: '等待审批',
  waiting_input: '等待补充',
  paused: '已暂停',
  succeeded: '已完成',
  failed: '已失败',
  cancelled: '已取消',
  interrupted: '已中断',
}

const STEP_STATUS_TEXT: Record<string, string> = {
  pending: '待执行',
  ready: '就绪',
  running: '进行中',
  waiting_approval: '等待审批',
  succeeded: '完成',
  failed: '失败',
  skipped: '跳过',
  cancelled: '取消',
}

function statusText(status: string): string {
  return STATUS_TEXT[status] || status
}

function statusChipClass(status: string): string {
  switch (status) {
    case 'succeeded':
      return 'chip success'
    case 'failed':
      return 'chip danger'
    case 'cancelled':
    case 'interrupted':
      return 'chip muted'
    default:
      return 'chip info'
  }
}

function artifactKindText(kind: string): string {
  switch (kind) {
    case 'file_create':
      return '新建'
    case 'file_update':
      return '修改'
    default:
      return kind
  }
}

export function AITasksView() {
  const [tasks, setTasks] = useState<api.AITask[]>([])
  const [loading, setLoading] = useState(true)
  const [expanded, setExpanded] = useState<string | null>(null)
  const [cancelTarget, setCancelTarget] = useState<api.AITask | null>(null)
  const [cancelling, setCancelling] = useState(false)
  const { toast } = useToast()

  const fetchData = useCallback(async () => {
    try {
      const res = await api.getAITasks(100)
      setTasks(res.tasks || [])
    } catch {
      // AI 插件未启用或接口不可用时保持空列表，由空态文案说明。
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const handleCancel = useCallback(async () => {
    if (!cancelTarget) return
    setCancelling(true)
    try {
      await api.cancelAITask(cancelTarget.id)
      toast(`任务「${cancelTarget.goal || cancelTarget.id}」已取消`, 'success')
      fetchData()
    } catch (e: unknown) {
      toast(`取消失败: ${(e as Error).message}`, 'error')
    } finally {
      setCancelling(false)
      setCancelTarget(null)
    }
  }, [cancelTarget, fetchData, toast])

  if (loading) return <div className="loading">加载中...</div>

  return (
    <div className="section">
      <div className="section-header">
        <h2><Icon name="activity" size={18} /> AI 任务 <span className="plugin-count">({tasks.length})</span></h2>
        <button className="btn-secondary" onClick={fetchData}><Icon name="refresh" size={13} />刷新</button>
      </div>

      {tasks.length === 0 && (
        <p className="empty">暂无任务记录（AI 插件未启用，或还没有复杂任务创建计划）</p>
      )}

      {tasks.length > 0 && (
        <div className="card">
          <div className="table-wrap">
            <table className="data-table">
              <thead>
                <tr>
                  <th>状态</th>
                  <th>目标</th>
                  <th>来源</th>
                  <th>计划</th>
                  <th>产物</th>
                  <th>更新时间</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {tasks.map((task) => (
                  <Fragment key={task.id}>
                    <tr
                      onClick={() => setExpanded(expanded === task.id ? null : task.id)}
                      style={{ cursor: 'pointer' }}
                    >
                      <td><span className={statusChipClass(task.status)}>{statusText(task.status)}</span></td>
                      <td>{task.goal || '（无目标描述）'}</td>
                      <td className="mono">{task.platform}:{task.chat_id}</td>
                      <td className="mono">v{task.plan_version}</td>
                      <td>{task.artifacts?.length || 0}</td>
                      <td>{new Date(task.updated_at).toLocaleString()}</td>
                      <td>
                        {ACTIVE_STATUSES.has(task.status) && (
                          <button
                            className="btn-secondary btn-sm"
                            onClick={(e) => { e.stopPropagation(); setCancelTarget(task) }}
                          >
                            <Icon name="stop" size={12} />取消
                          </button>
                        )}
                      </td>
                    </tr>
                    {expanded === task.id && (
                      <tr>
                        <td colSpan={7}>
                          <div className="task-detail">
                            <div className="mono" style={{ fontSize: '0.72rem', color: 'var(--text-dim)' }}>
                              ID: {task.id} · 发起者: {task.owner_id} · 创建: {new Date(task.created_at).toLocaleString()}
                            </div>
                            {(task.steps?.length || 0) > 0 && (
                              <ul className="task-steps">
                                {task.steps!.map((step) => (
                                  <li key={step.id}>
                                    <span className="mono">[{STEP_STATUS_TEXT[step.status] || step.status}]</span>{' '}
                                    {step.description}
                                    {step.result ? <span className="task-note"> — {step.result}</span> : null}
                                  </li>
                                ))}
                              </ul>
                            )}
                            {(task.artifacts?.length || 0) > 0 && (
                              <div className="task-artifacts">
                                <strong>产物：</strong>
                                {task.artifacts!.map((a, i) => (
                                  <span key={`${a.kind}-${a.name}-${i}`} className="chip muted">
                                    {artifactKindText(a.kind)} {a.name}
                                  </span>
                                ))}
                              </div>
                            )}
                          </div>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={!!cancelTarget}
        title="取消 AI 任务"
        message={`确定要取消任务「${cancelTarget?.goal || cancelTarget?.id}」吗？已完成的步骤会保留为记录。`}
        confirmLabel="取消任务"
        confirmVariant="danger"
        loading={cancelling}
        onConfirm={handleCancel}
        onCancel={() => setCancelTarget(null)}
      />
    </div>
  )
}
