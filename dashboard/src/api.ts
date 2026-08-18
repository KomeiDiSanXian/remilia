const STORAGE_KEY = 'remilia_api_key'

export function getStoredApiKey(): string | null {
  return localStorage.getItem(STORAGE_KEY)
}

export function setStoredApiKey(key: string): void {
  localStorage.setItem(STORAGE_KEY, key)
}

export function clearApiKey(): void {
  localStorage.removeItem(STORAGE_KEY)
}

export let baseURL = ''

export function setBaseURL(url: string): void {
  baseURL = url.replace(/\/+$/, '')
}

interface APIResponse<T> {
  code: number
  message: string
  data: T | null
}

async function request<T>(method: string, path: string, body?: unknown, timeout = 5000): Promise<T> {
  const apiKey = getStoredApiKey()
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeout)

  try {
    const res = await fetch(`${baseURL}${path}`, {
      signal: controller.signal,
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(apiKey ? { Authorization: `Bearer ${apiKey}` } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    })
    clearTimeout(timer)

    if (!res.ok) {
      throw new Error(`HTTP ${res.status}: ${res.statusText}`)
    }

    const json: APIResponse<T> = await res.json()
    if (json.code !== 0) {
      throw new Error(json.message || `error ${json.code}`)
    }
    return json.data as T
  } catch (err) {
    clearTimeout(timer)
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw new Error(`请求超时 (${path})`)
    }
    throw err
  }
}

// --- Types ---

export interface BotInfo {
  name: string
  status: 'running' | 'stopped'
  uptime: string
  version: string
  platforms: string[]
  plugin_count: number
}

export interface PluginInfo {
  name: string
  state: string
  version: string
  uptime: string
  dependencies: string[]
  matcher_count: number
  last_error?: string
  load_time?: string
}

export interface VersionInfo {
  version: string
  commit: string
  build_date: string
  go_version: string
}

// --- Bot API ---

export async function listBots(): Promise<BotInfo[]> {
  return request<BotInfo[]>('GET', '/api/v1/bots')
}

export async function getBot(name: string): Promise<BotInfo> {
  return request<BotInfo>('GET', `/api/v1/bots/${name}`)
}

export async function startBot(name: string): Promise<void> {
  await request('POST', `/api/v1/bots/${name}/start`)
}

export async function stopBot(name: string): Promise<void> {
  await request('POST', `/api/v1/bots/${name}/stop`)
}

export async function restartBot(name: string): Promise<void> {
  await request('POST', `/api/v1/bots/${name}/restart`)
}

// --- Plugin API ---

export async function listPlugins(): Promise<PluginInfo[]> {
  return request<PluginInfo[]>('GET', '/api/v1/plugins')
}

export async function enablePlugin(name: string): Promise<void> {
  await request('POST', `/api/v1/plugins/${name}/enable`)
}

export async function disablePlugin(name: string): Promise<void> {
  await request('POST', `/api/v1/plugins/${name}/disable`)
}

export async function reloadPlugin(name: string): Promise<void> {
  await request('POST', `/api/v1/plugins/${name}/reload`)
}

export async function getPlugin(name: string): Promise<PluginInfo> {
  return request<PluginInfo>('GET', `/api/v1/plugins/${name}`)
}

export interface PluginDetail {
  Name: string
  State: string
  LoadTime: string
  LastError: string | null
  MatcherCount: number
  Uptime: number
  HasSaveState: boolean
  EventBusSubscriptions: number
  GoroutineCount: number
  Metadata: PluginMetadata | null
}

export interface PluginMetadata {
  Name: string
  Version: string
  Dependencies: string[]
  Author: string
  Description: string
  HelpText: string
  Category: string
  Tags: string[]
  Homepage: string
  Repository: string
  Hidden: boolean
}

export async function getPluginDetail(name: string): Promise<PluginDetail> {
  return request<PluginDetail>('GET', `/api/v1/plugins/${name}`)
}

// --- System API ---

export async function getVersion(): Promise<VersionInfo> {
  return request<VersionInfo>('GET', '/api/v1/version')
}

export async function getHealth(): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>('GET', '/api/v1/health', undefined, 15000)
}

// --- Platform API (Phase A) ---

export interface PlatformInfo {
  name: string
  running: boolean
  bot_id?: string
  bot_name?: string
  capabilities?: Record<string, boolean>
}

export async function listPlatforms(): Promise<PlatformInfo[]> {
  return request<PlatformInfo[]>('GET', '/api/v1/platforms')
}

export async function getPlatform(name: string): Promise<PlatformInfo> {
  return request<PlatformInfo>('GET', `/api/v1/platforms/${name}`)
}

export interface AddPlatformRequest {
  type: string
  config: Record<string, unknown>
}

export async function addPlatform(data: AddPlatformRequest): Promise<void> {
  await request('POST', '/api/v1/platforms', data)
}

export async function deletePlatform(name: string): Promise<void> {
  await request('DELETE', `/api/v1/platforms/${name}`)
}

// --- Engine API (Phase A) ---

export interface CommandInfo {
  command: string
  description: string
  usage: string
  aliases: string[]
  examples?: string[]
  permissions?: string[]
  category: string
  plugin: string
}

export interface MatcherStats {
  total: number
  global: number
  by_plugin: Record<string, number>
  global_enabled: boolean
  temp: number
}

export async function listCommands(): Promise<CommandInfo[]> {
  return request<CommandInfo[]>('GET', '/api/v1/engine/commands')
}

export async function getMatcherStats(): Promise<MatcherStats> {
  return request<MatcherStats>('GET', '/api/v1/engine/matchers')
}

export interface MatcherGroupInfo {
  name: string
  count: number
  enabled: boolean
}

export async function listMatcherGroups(): Promise<MatcherGroupInfo[]> {
  const data = await request<{ groups: MatcherGroupInfo[] }>('GET', '/api/v1/engine/matchers/groups')
  return data.groups ?? []
}

export async function disableMatcherGroup(name: string): Promise<void> {
  await request('POST', `/api/v1/engine/matchers/group/${name}/disable`)
}

export async function enableMatcherGroup(name: string): Promise<void> {
  await request('POST', `/api/v1/engine/matchers/group/${name}/enable`)
}

// --- Audit Log API (Phase A) ---

export interface AuditEntry {
  id: number
  timestamp: string
  user_id: string
  group_id?: string
  action: string
  content?: string
  meta?: Record<string, unknown>
}

export interface AuditLogResponse {
  entries: AuditEntry[]
  count: number
  total?: number
}

export async function getAuditLog(n = 50): Promise<AuditLogResponse> {
  return request<AuditLogResponse>('GET', `/api/v1/auditlog?n=${n}`)
}

export async function getAuditLogByUser(userId: string, n = 50): Promise<AuditLogResponse> {
  return request<AuditLogResponse>('GET', `/api/v1/auditlog/user/${userId}?n=${n}`)
}

export async function getAuditLogByAction(action: string, n = 50): Promise<AuditLogResponse> {
  return request<AuditLogResponse>('GET', `/api/v1/auditlog/action/${action}?n=${n}`)
}

export async function getAuditLogCount(): Promise<{ total: number }> {
  return request<{ total: number }>('GET', '/api/v1/auditlog/count')
}

// --- Permission API ---

export async function listRoles(): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>('GET', '/api/v1/permission/roles')
}

export async function assignRole(userId: string, role: string): Promise<void> {
  await request('POST', `/api/v1/permission/users/${userId}/roles`, { role })
}

export async function revokeRole(userId: string, role: string): Promise<void> {
  await request('DELETE', `/api/v1/permission/users/${userId}/roles/${role}`)
}

export async function getUserPermissions(userId: string): Promise<{ roles: string[]; permissions: { resource: string; action: string }[] }> {
  return request('GET', `/api/v1/permission/users/${userId}/permissions`)
}

export async function checkPermission(userId: string, resource: string, action: string): Promise<{ allowed: boolean }> {
  return request('POST', '/api/v1/permission/check', { user_id: userId, resource, action })
}

export async function createRole(name: string, permissions: string[]): Promise<void> {
  await request('POST', '/api/v1/permission/roles', { name, permissions })
}

export async function deleteRole(name: string): Promise<void> {
  await request('DELETE', `/api/v1/permission/roles/${name}`)
}

export async function addRolePermission(role: string, resource: string, action: string): Promise<void> {
  await request('POST', `/api/v1/permission/roles/${role}/permissions`, { resource, action })
}

export async function removeRolePermission(role: string, resource: string, action: string): Promise<void> {
  await request('DELETE', `/api/v1/permission/roles/${role}/permissions`, { resource, action })
}

// --- FSM API ---

export async function listFSMs(): Promise<{ fsms: string[] }> {
  return request<{ fsms: string[] }>('GET', '/api/v1/fsm')
}

export interface FSMSummary {
  name: string
  initial: string
  timeout: string
}

export async function getFSMDetail(name: string): Promise<FSMSummary> {
  return request<FSMSummary>('GET', `/api/v1/fsm/${encodeURIComponent(name)}`)
}

export interface FSMSessionInfo {
  id: string
  fsm_name: string
  current: string
  created_at: number
  updated_at: number
  expire_at?: number
}

export async function listFSMSessions(): Promise<FSMSessionInfo[]> {
  const data = await request<{ sessions: FSMSessionInfo[] }>('GET', '/api/v1/fsm/sessions')
  return data.sessions ?? []
}

export async function endFSMSession(id: string): Promise<void> {
  await request('DELETE', `/api/v1/fsm/sessions/${encodeURIComponent(id)}`)
}

// --- Scheduler API ---

export interface JobRecord {
  job_id: number
  job_name: string
  start_at: string
  duration: string
  success: boolean
  error?: string
}

export interface SchedulerJobInfo {
  id: number
  name: string
  kind: 'cron' | 'ticker'
}

export async function getSchedulerJobs(): Promise<{ count: number; jobs: SchedulerJobInfo[] }> {
  return request<{ count: number; jobs: SchedulerJobInfo[] }>('GET', '/api/v1/scheduler/jobs')
}

export async function getSchedulerHistory(n = 50): Promise<{ history: JobRecord[]; count: number }> {
  return request('GET', `/api/v1/scheduler/history?n=${n}`)
}

export async function deleteSchedulerJob(id: number): Promise<void> {
  await request('DELETE', `/api/v1/scheduler/jobs/${id}`)
}

// --- Config API ---

export async function getConfig(): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>('GET', '/api/v1/config')
}

export async function updateConfig(config: Record<string, unknown>): Promise<void> {
  await request('PUT', '/api/v1/config', config)
}

export async function reloadConfig(): Promise<void> {
  await request('POST', '/api/v1/config/reload')
}

// --- Log API ---

export interface LogEntry {
  time: string
  level: string
  message: string
}

export async function getLogs(n = 100): Promise<{ entries: LogEntry[] }> {
  return request('GET', `/api/v1/logs?n=${n}`)
}

// --- System Stats ---

export interface SystemStats {
  plugins_total: number
  plugins_by_state?: Record<string, number>
  load_order?: string[]
  goroutine_summary?: { total: number; by_plugin?: Record<string, number> }
  draining_count?: number
  container_services?: number
  strict_deps?: boolean
  uptime?: string
}

export async function getStats(): Promise<SystemStats> {
  return request<SystemStats>('GET', '/api/v1/stats')
}
