import { useEffect, useState, useCallback } from 'react'
import * as api from '../api'
import { ConfigForm } from './ConfigForm'
import { Icon } from './Icons.tsx'

type Status = 'idle' | 'loading' | 'saving' | 'saved' | 'error'
type Mode = 'form' | 'raw'

const SENSITIVE_KEYS = new Set(['token', 'secret', 'api_key', 'password', 'access_token'])

function isSensitive(key: string): boolean {
  return SENSITIVE_KEYS.has(key)
}

function buildPatch(original: Record<string, unknown>, edited: Record<string, unknown>): Record<string, unknown> {
  const patch: Record<string, unknown> = {}
  for (const [key, value] of Object.entries(edited)) {
    if (isSensitive(key)) {
      const origVal = original[key]
      if (value === '' || value === origVal) continue
      patch[key] = value
    } else if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
      const origObj = original[key] as Record<string, unknown> | undefined
      if (origObj) {
        const nested = buildPatch(origObj, value as Record<string, unknown>)
        if (Object.keys(nested).length > 0) patch[key] = nested
      } else {
        patch[key] = value
      }
    } else if (value !== original[key]) {
      patch[key] = value
    }
  }
  return patch
}

export function ConfigEditor() {
  const [mode, setMode] = useState<Mode>('form')
  const [originalConfig, setOriginalConfig] = useState<Record<string, unknown> | null>(null)
  const [editConfig, setEditConfig] = useState<Record<string, unknown> | null>(null)
  const [originalText, setOriginalText] = useState('')
  const [editText, setEditText] = useState('')
  const [status, setStatus] = useState<Status>('idle')
  const [statusMessage, setStatusMessage] = useState('')

  const fetchConfig = useCallback(async () => {
    setStatus('loading')
    setStatusMessage('')
    try {
      const cfg = await api.getConfig()
      setOriginalConfig(cfg)
      setEditConfig(structuredClone(cfg))
      const text = JSON.stringify(cfg, null, 2)
      setOriginalText(text)
      setEditText(text)
      setStatus('idle')
    } catch (e: unknown) {
      setStatus('error')
      setStatusMessage('获取配置失败: ' + (e as Error).message)
    }
  }, [])

  useEffect(() => { fetchConfig() }, [fetchConfig])

  const modified = mode === 'raw'
    ? editText !== originalText
    : JSON.stringify(editConfig) !== JSON.stringify(originalConfig)

  const handleSave = useCallback(async () => {
    if (mode === 'raw') {
      let parsed: Record<string, unknown>
      try { parsed = JSON.parse(editText) }
      catch (e: unknown) {
        setStatus('error')
        setStatusMessage('JSON 格式错误: ' + (e as Error).message)
        return
      }
      setStatus('saving')
      setStatusMessage('')
      try {
        await api.updateConfig(parsed)
        setOriginalText(editText)
        setOriginalConfig(parsed)
        setStatus('saved')
        setStatusMessage('配置已保存并生效')
        setTimeout(() => { setStatus('idle'); setStatusMessage('') }, 3000)
      } catch (e: unknown) {
        setStatus('error')
        setStatusMessage('保存失败: ' + (e as Error).message)
      }
      return
    }

    if (!originalConfig || !editConfig) return
    setStatus('saving')
    setStatusMessage('')
    try {
      const patch = buildPatch(originalConfig, editConfig)
      if (Object.keys(patch).length === 0) {
        setStatus('idle')
        setStatusMessage('没有需要保存的修改')
        setTimeout(() => setStatusMessage(''), 2000)
        return
      }
      await api.updateConfig(patch)
      setOriginalConfig(structuredClone(editConfig))
      setOriginalText(JSON.stringify(editConfig, null, 2))
      setStatus('saved')
      setStatusMessage('配置已保存并生效')
      setTimeout(() => { setStatus('idle'); setStatusMessage('') }, 3000)
    } catch (e: unknown) {
      setStatus('error')
      setStatusMessage('保存失败: ' + (e as Error).message)
    }
  }, [mode, editText, originalConfig, editConfig])

  const handleReload = useCallback(async () => {
    setStatus('loading')
    setStatusMessage('')
    try {
      await api.reloadConfig()
      setStatusMessage('配置已从磁盘重新加载')
      await fetchConfig()
      setStatus('saved')
      setTimeout(() => { setStatus('idle'); setStatusMessage('') }, 3000)
    } catch (e: unknown) {
      setStatus('error')
      setStatusMessage('重载失败: ' + (e as Error).message)
    }
  }, [fetchConfig])

  // 未保存提示：关闭窗口 / 刷新页面时阻止
  useEffect(() => {
    if (!modified) return
    const handler = (e: BeforeUnloadEvent) => { e.preventDefault() }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [modified])

  const handleReset = useCallback(() => {
    if (mode === 'raw') {
      setEditText(originalText)
    } else {
      setEditConfig(originalConfig ? structuredClone(originalConfig) : null)
    }
    setStatus('idle')
    setStatusMessage('')
  }, [mode, originalText, originalConfig])

  const handleModeSwitch = useCallback((newMode: Mode) => {
    if (newMode === mode) return
    if (newMode === 'raw' && editConfig) {
      setEditText(JSON.stringify(editConfig, null, 2))
    }
    setMode(newMode)
  }, [mode, editConfig])

  const statusClass =
    status === 'error' ? 'status-error' :
    status === 'saved' ? 'status-ok' : ''

  return (
    <div className="section">
      <div className="section-header">
        <h2>配置编辑</h2>
        <div className="config-actions">
          <button className="btn-secondary" onClick={fetchConfig} disabled={status === 'loading' || status === 'saving'}>
            <Icon name="refresh" size={13} />刷新
          </button>
          <button className="btn-secondary" onClick={handleReload} disabled={status === 'loading' || status === 'saving'}>
            <Icon name="download" size={13} />从磁盘重新加载
          </button>
        </div>
      </div>

      {statusMessage && (
        <div className={`banner ${statusClass}`}>{statusMessage}</div>
      )}

      <div className="config-mode-tabs">
        <button className={`config-mode-tab ${mode === 'form' ? 'active' : ''}`} onClick={() => handleModeSwitch('form')}>表单</button>
        <button className={`config-mode-tab ${mode === 'raw' ? 'active' : ''}`} onClick={() => handleModeSwitch('raw')}>原始 JSON</button>
      </div>

      <div className="config-toolbar">
        <span className={`config-status ${statusClass}`}>
          {status === 'loading' && '加载中...'}
          {status === 'saving' && '保存中...'}
          {status === 'saved' && '已保存'}
          {status === 'error' && '错误'}
          {status === 'idle' && (modified ? '有未保存的修改' : '无修改')}
        </span>
        <div className="config-toolbar-actions">
          {modified && (
            <button className="btn-secondary" onClick={handleReset}>重置</button>
          )}
          <button onClick={handleSave} disabled={!modified || status === 'saving'}>保存</button>
        </div>
      </div>

      {mode === 'raw' ? (
        <textarea className="config-editor" value={editText} onChange={(e) => setEditText(e.target.value)} spellCheck={false} readOnly={status === 'saving'} />
      ) : (
        editConfig && <ConfigForm value={editConfig} onChange={setEditConfig} />
      )}
    </div>
  )
}
