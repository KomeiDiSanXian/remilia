import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'
import { useToast } from './Toast.tsx'
import { EmptyState } from './EmptyState.tsx'

interface RoleDef {
  name: string
  permissions: string[]
}

interface PermResponse {
  roles: RoleDef[]
  user_roles: Record<string, string[]>
}

export function PermissionView() {
  const [data, setData] = useState<PermResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [activeTab, setActiveTab] = useState<'roles' | 'users'>('users')
  const [showCreateRole, setShowCreateRole] = useState(false)
  const [newRoleName, setNewRoleName] = useState('')
  const [showAssignRole, setShowAssignRole] = useState<{ userId: string; roles: string[] } | null>(null)
  const [assignRoleName, setAssignRoleName] = useState('')
  const [showAddPerm, setShowAddPerm] = useState<string | null>(null)
  const [permResource, setPermResource] = useState('')
  const [permAction, setPermAction] = useState('')
  const [addingPerm, setAddingPerm] = useState(false)
  const { toast } = useToast()

  const fetchData = useCallback(async () => {
    try {
      const d = await api.listRoles() as unknown as PermResponse
      setData(d)
      setError('')
    } catch (e: unknown) { setError((e as Error).message) }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { fetchData() }, [fetchData])

  const handleCreateRole = useCallback(async () => {
    if (!newRoleName.trim()) return
    try {
      await api.createRole(newRoleName.trim(), [])
      toast(`角色「${newRoleName.trim()}」已创建`, 'success')
      setShowCreateRole(false)
      setNewRoleName('')
      fetchData()
    } catch (e: unknown) { toast(`创建失败: ${(e as Error).message}`, 'error') }
  }, [newRoleName, fetchData, toast])

  const handleDeleteRole = useCallback(async (name: string) => {
    try {
      await api.deleteRole(name)
      toast(`角色「${name}」已删除`, 'success')
      fetchData()
    } catch (e: unknown) { toast(`删除失败: ${(e as Error).message}`, 'error') }
  }, [fetchData, toast])

  const handleAssignRole = useCallback(async () => {
    if (!showAssignRole || !assignRoleName.trim()) return
    try {
      await api.assignRole(showAssignRole.userId, assignRoleName.trim())
      toast('角色已分配', 'success')
      setAssignRoleName('')
      fetchData()
    } catch (e: unknown) { toast(`分配失败: ${(e as Error).message}`, 'error') }
  }, [showAssignRole, assignRoleName, fetchData, toast])

  const handleRevokeRole = useCallback(async (userId: string, role: string) => {
    try {
      await api.revokeRole(userId, role)
      toast(`已撤销角色「${role}」`, 'success')
      fetchData()
    } catch (e: unknown) { toast(`撤销失败: ${(e as Error).message}`, 'error') }
  }, [fetchData, toast])

  const handleAddPerm = useCallback(async () => {
    if (!showAddPerm || !permResource.trim()) return
    setAddingPerm(true)
    try {
      await api.addRolePermission(showAddPerm, permResource.trim(), permAction.trim() || '*')
      toast('权限已添加', 'success')
      setPermResource('')
      setPermAction('')
      fetchData()
    } catch (e: unknown) { toast(`添加失败: ${(e as Error).message}`, 'error') }
    finally { setAddingPerm(false) }
  }, [showAddPerm, permResource, permAction, fetchData, toast])

  const handleRemovePerm = useCallback(async (role: string, resource: string, action: string) => {
    try {
      await api.removeRolePermission(role, resource, action)
      toast('权限已移除', 'success')
      fetchData()
    } catch (e: unknown) { toast(`移除失败: ${(e as Error).message}`, 'error') }
  }, [fetchData, toast])

  if (loading) return <div className="section"><h2>权限管理</h2><div className="loading">加载中...</div></div>
  if (error) return <div className="error-card">获取失败: {error}</div>
  if (!data) return null

  const allRoles = data.roles?.map((r) => r.name) || []
  const userEntries = Object.entries(data.user_roles || {})

  return (
    <div className="section">
      <div className="section-header">
        <h2>权限管理</h2>
        <div className="permission-hint">权限修改即时生效，无需重启</div>
      </div>

      <div className="config-mode-tabs" style={{ marginBottom: '1rem' }}>
        <button className={`config-mode-tab ${activeTab === 'users' ? 'active' : ''}`} onClick={() => setActiveTab('users')}>用户分配 ({userEntries.length})</button>
        <button className={`config-mode-tab ${activeTab === 'roles' ? 'active' : ''}`} onClick={() => setActiveTab('roles')}>角色定义 ({data.roles?.length || 0})</button>
      </div>

      {activeTab === 'users' && (
        <>
          <div style={{ marginBottom: '0.75rem' }}>
            <button onClick={() => setShowAssignRole({ userId: '', roles: [] })}>分配角色</button>
          </div>
          {userEntries.length === 0
            ? <EmptyState message="暂无用户-角色分配" hint="点击「分配角色」为指定用户分配角色。" />
            : userEntries.map(([userId, roleList]) => (
              <div key={userId} className="card">
                <div className="card-header">
                  <strong>{userId}</strong>
                  <button className="btn-secondary" onClick={() => setShowAssignRole({ userId, roles: roleList })}>管理</button>
                </div>
                <div className="card-body">
                  {roleList.length === 0
                    ? <span className="empty" style={{ padding: '0.5rem 0' }}>无角色</span>
                    : roleList.map((r) => (
                      <div key={r} className="info-row">
                        <span className="label">{r}</span>
                        <button className="btn-secondary" style={{ fontSize: '0.7rem', padding: '0.15rem 0.4rem' }} onClick={() => handleRevokeRole(userId, r)}>撤销</button>
                      </div>
                    ))}
                </div>
              </div>
            ))}
        </>
      )}

      {activeTab === 'roles' && (
        <>
          <div style={{ marginBottom: '0.75rem' }}>
            <button onClick={() => setShowCreateRole(true)}>创建角色</button>
          </div>
          {(!data.roles || data.roles.length === 0)
            ? <EmptyState message="暂无角色定义" hint="点击「创建角色」创建一个新角色。" />
            : data.roles.map((role) => (
              <div key={role.name} className="card">
                <div className="card-header">
                  <strong>{role.name}</strong>
                  <button className="warn" style={{ fontSize: '0.75rem', padding: '0.2rem 0.5rem' }} onClick={() => handleDeleteRole(role.name)}>删除</button>
                </div>
                <div className="card-body">
                  {(!role.permissions || role.permissions.length === 0) && <div className="empty" style={{ padding: '0.5rem 0' }}>无权限</div>}
                  {role.permissions?.map((p) => {
                    const [res, act = '*'] = p.split(':')
                    return (
                      <div key={p} className="info-row">
                        <span className="label">{res}</span>
                        <span>{act}</span>
                        <button className="btn-secondary" style={{ fontSize: '0.7rem', padding: '0.15rem 0.4rem' }} onClick={() => handleRemovePerm(role.name, res, act)}>移除</button>
                      </div>
                    )
                  })}
                  <button className="btn-secondary" style={{ marginTop: '0.5rem' }} onClick={() => { setShowAddPerm(role.name); setPermResource(''); setPermAction('') }}>添加权限</button>
                </div>
              </div>
            ))}
        </>
      )}

      {/* --- Create Role Dialog --- */}
      {showCreateRole && (
        <div className="dialog-overlay" onClick={() => setShowCreateRole(false)}>
          <div className="dialog" onClick={(e) => e.stopPropagation()}>
            <div className="dialog-header">创建角色</div>
            <div className="dialog-body">
              <div className="field">
                <label>角色名称</label>
                <input type="text" value={newRoleName} onChange={(e) => setNewRoleName(e.target.value)} placeholder="例如: moderator" autoFocus />
              </div>
            </div>
            <div className="dialog-actions">
              <button className="btn-secondary" onClick={() => setShowCreateRole(false)}>取消</button>
              <button onClick={handleCreateRole} disabled={!newRoleName.trim()}>创建</button>
            </div>
          </div>
        </div>
      )}

      {/* --- Assign Role Dialog --- */}
      {showAssignRole && (
        <div className="dialog-overlay" onClick={() => setShowAssignRole(null)}>
          <div className="dialog" onClick={(e) => e.stopPropagation()}>
            <div className="dialog-header">分配角色</div>
            <div className="dialog-body">
              {showAssignRole.userId && (
                <div className="field">
                  <label>用户</label>
                  <input type="text" value={showAssignRole.userId} readOnly />
                </div>
              )}
              {!showAssignRole.userId && (
                <div className="field">
                  <label>用户 ID</label>
                  <input type="text" value={showAssignRole.userId} onChange={(e) => setShowAssignRole({ userId: e.target.value, roles: [] })} placeholder="输入用户 ID" autoFocus />
                </div>
              )}
              <div className="field">
                <label>角色</label>
                <select value={assignRoleName} onChange={(e) => setAssignRoleName(e.target.value)}>
                  <option value="">选择角色...</option>
                  {allRoles.filter((r) => !showAssignRole.roles.includes(r)).map((r) => (
                    <option key={r} value={r}>{r}</option>
                  ))}
                </select>
              </div>
              {showAssignRole.roles.length > 0 && (
                <div style={{ fontSize: '0.8rem', color: '#64748b' }}>
                  已有角色: {showAssignRole.roles.join(', ')}
                </div>
              )}
            </div>
            <div className="dialog-actions">
              <button className="btn-secondary" onClick={() => setShowAssignRole(null)}>取消</button>
              <button onClick={handleAssignRole} disabled={!showAssignRole.userId || !assignRoleName}>分配</button>
            </div>
          </div>
        </div>
      )}

      {/* --- Add Permission Dialog --- */}
      {showAddPerm && (
        <div className="dialog-overlay" onClick={() => setShowAddPerm(null)}>
          <div className="dialog" onClick={(e) => e.stopPropagation()}>
            <div className="dialog-header">添加权限 — {showAddPerm}</div>
            <div className="dialog-body">
              <div className="field">
                <label>资源 (Resource)</label>
                <input type="text" value={permResource} onChange={(e) => setPermResource(e.target.value)} placeholder="例如: command:weather, command:* 或 *" autoFocus />
              </div>
              <div className="field">
                <label>动作 (Action)</label>
                <input type="text" value={permAction} onChange={(e) => setPermAction(e.target.value)} placeholder="例如: execute, manage, view 或 * (通配)" />
              </div>
              <div style={{ fontSize: '0.8rem', color: '#64748b', marginTop: '0.5rem' }}>
                权限格式: <code>resource:action</code>，支持通配符 <code>*</code>
              </div>
            </div>
            <div className="dialog-actions">
              <button className="btn-secondary" onClick={() => setShowAddPerm(null)} disabled={addingPerm}>取消</button>
              <button onClick={handleAddPerm} disabled={!permResource.trim() || addingPerm}>
                {addingPerm ? '添加中...' : '添加'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
