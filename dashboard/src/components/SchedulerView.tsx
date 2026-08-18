import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'
import { useToast } from './Toast.tsx'
import { ConfirmDialog } from './ConfirmDialog.tsx'
import { Icon } from './Icons.tsx'

export function SchedulerView() {
  const [jobs, setJobs] = useState<api.SchedulerJobInfo[]>([])
  const [history, setHistory] = useState<api.JobRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [deleteTarget, setDeleteTarget] = useState<api.SchedulerJobInfo | null>(null)
  const [deleting, setDeleting] = useState(false)
  const { toast } = useToast()

  const fetchData = useCallback(async () => {
    try {
      const [j, h] = await Promise.all([api.getSchedulerJobs(), api.getSchedulerHistory(100)])
      setJobs(j.jobs || [])
      setHistory(h.history)
    } catch { /* ignore */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await api.deleteSchedulerJob(deleteTarget.id)
      toast(`任务「${deleteTarget.name}」已移除`, 'success')
      fetchData()
    } catch (e: unknown) {
      toast(`删除失败: ${(e as Error).message}`, 'error')
    } finally {
      setDeleting(false)
      setDeleteTarget(null)
    }
  }, [deleteTarget, fetchData, toast])

  if (loading) return <div className="loading">加载中...</div>

  return (
    <div className="section">
      <div className="section-header">
        <h2>计划任务 <span className="plugin-count">({jobs.length})</span></h2>
        <button className="btn-secondary" onClick={fetchData}><Icon name="refresh" size={13} />刷新</button>
      </div>

      {jobs.length === 0 && <p className="empty">暂无注册的计划任务</p>}

      {jobs.length > 0 && (
        <div className="card">
          {jobs.map((job, i) => (
            <div className="job-row" key={job.id} style={i === 0 ? { borderTop: 'none' } : undefined}>
              <span className="status-dot running" />
              <span className="job-name">{job.name || `任务 #${job.id}`}</span>
              <span className={`chip ${job.kind === 'cron' ? 'info' : 'accent'}`}>
                <Icon name={job.kind === 'cron' ? 'clock' : 'restart'} size={11} />
                {job.kind === 'cron' ? 'cron' : 'ticker'}
              </span>
              <span className="chip muted">#{job.id}</span>
              <button className="btn-secondary btn-sm" onClick={() => setDeleteTarget(job)}>
                <Icon name="trash" size={12} />移除
              </button>
            </div>
          ))}
        </div>
      )}

      <div className="section-header" style={{ marginTop: '1.2rem' }}>
        <h2><Icon name="activity" size={18} />执行历史 <span className="plugin-count">({history.length})</span></h2>
      </div>

      {history.length === 0 && <p className="empty">暂无执行记录</p>}

      {history.length > 0 && (
        <div className="card">
          <table className="data-table">
            <thead>
              <tr><th>任务</th><th>结果</th><th>执行时间</th><th>耗时</th><th>错误</th></tr>
            </thead>
            <tbody>
              {history.map((rec, i) => (
                <tr key={`${rec.job_id}-${i}`}>
                  <td>{rec.job_name || `#${rec.job_id}`}</td>
                  <td>
                    <span className={`chip ${rec.success ? 'success' : 'danger'}`}>
                      <Icon name={rec.success ? 'check' : 'alert'} size={11} />
                      {rec.success ? '成功' : '失败'}
                    </span>
                  </td>
                  <td>{new Date(rec.start_at).toLocaleString()}</td>
                  <td className="mono">{rec.duration}</td>
                  <td style={{ color: 'var(--danger)', fontSize: '0.78rem', wordBreak: 'break-word' }}>{rec.error || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ConfirmDialog
        open={!!deleteTarget}
        title="移除计划任务"
        message={`确定要移除任务「${deleteTarget?.name}」吗？`}
        confirmLabel="移除"
        confirmVariant="danger"
        loading={deleting}
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />
    </div>
  )
}
