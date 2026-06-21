import { useState } from 'react'

interface ConfigFormProps {
  value: Record<string, unknown>
  onChange: (value: Record<string, unknown>) => void
}

const SENSITIVE_KEYS = new Set(['token', 'secret', 'api_key', 'password', 'access_token'])

function isSensitive(key: string): boolean {
  return SENSITIVE_KEYS.has(key)
}

export function ConfigForm({ value, onChange }: ConfigFormProps) {
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const [editingSensitive, setEditingSensitive] = useState<Set<string>>(new Set())

  const toggle = (path: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }

  const setAtPath = (path: string, newVal: unknown) => {
    const keys = path.split('.')
    const clone = structuredClone(value)
    let obj = clone
    for (let i = 0; i < keys.length - 1; i++) {
      obj = obj[keys[i]] as Record<string, unknown>
    }
    obj[keys[keys.length - 1]] = newVal
    onChange(clone)
  }

  const renderField = (key: string, val: unknown, prefix: string): React.ReactNode => {
    const path = prefix ? `${prefix}.${key}` : key

    if (val === null || val === undefined) {
      return (
        <div className="cf-field" key={key}>
          <span className="cf-label">{key}</span>
          <span className="cf-null">null</span>
        </div>
      )
    }

    if (typeof val === 'boolean') {
      return (
        <div className="cf-field" key={key}>
          <span className="cf-label">{key}</span>
          <label className="cf-toggle">
            <input type="checkbox" checked={val} onChange={(e) => setAtPath(path, e.target.checked)} />
            <span>{val ? '开启' : '关闭'}</span>
          </label>
        </div>
      )
    }

    if (typeof val === 'number') {
      return (
        <div className="cf-field" key={key}>
          <span className="cf-label">{key}</span>
          <input type="number" value={val} onChange={(e) => setAtPath(path, e.target.value === '' ? '' : Number(e.target.value))} className="cf-input cf-input-narrow" />
        </div>
      )
    }

    if (typeof val === 'string') {
      if (isSensitive(key)) {
        const editing = editingSensitive.has(path)
        return (
          <div className="cf-field" key={key}>
            <span className="cf-label cf-label-sensitive">{key}</span>
            <div className="cf-sensitive-row">
              <input
                type="text"
                value={editing ? val : '********'}
                onChange={(e) => setAtPath(path, e.target.value)}
                onFocus={() => {
                  if (!editingSensitive.has(path)) {
                    setEditingSensitive((prev) => new Set(prev).add(path))
                    setAtPath(path, '')
                  }
                }}
                className="cf-input cf-input-sensitive"
                placeholder={editing ? '' : '点击编辑'}
              />
              {!editing && (
                <button className="cf-btn-small" onClick={() => {
                  setEditingSensitive((prev) => new Set(prev).add(path))
                  setAtPath(path, '')
                }}>修改</button>
              )}
            </div>
          </div>
        )
      }
      return (
        <div className="cf-field" key={key}>
          <span className="cf-label">{key}</span>
          {val.length > 60 ? (
            <textarea value={val} onChange={(e) => setAtPath(path, e.target.value)} className="cf-input cf-input-wide" rows={3} />
          ) : (
            <input type="text" value={val} onChange={(e) => setAtPath(path, e.target.value)} className="cf-input cf-input-wide" />
          )}
        </div>
      )
    }

    if (Array.isArray(val)) {
      const isEmpty = val.length === 0
      return (
        <div className="cf-section" key={key}>
          <div className="cf-section-header" onClick={() => toggle(path)}>
            <span className={`cf-arrow ${!collapsed.has(path) ? 'cf-expanded' : ''}`}>▶</span>
            <span className="cf-section-title">{key}</span>
            <span className="cf-section-meta">[{val.length}]</span>
          </div>
          {isEmpty && <div className="cf-section-body cf-empty">空数组</div>}
          {!isEmpty && !collapsed.has(path) && (
            <div className="cf-section-body">
              {val.map((item, i) => (
                <div className="cf-array-item" key={i}>
                  {typeof item === 'object' && item !== null
                    ? renderObject(item as Record<string, unknown>, `${path}[${i}]`)
                    : <span className="cf-value">{JSON.stringify(item)}</span>
                  }
                </div>
              ))}
            </div>
          )}
        </div>
      )
    }

    if (typeof val === 'object') {
      const entries = Object.entries(val as Record<string, unknown>)
      if (entries.length === 0) {
        return (
          <div className="cf-field" key={key}>
            <span className="cf-label">{key}</span>
            <span className="cf-empty">{'{}'}</span>
          </div>
        )
      }
      return (
        <div className={`cf-section ${prefix === '' ? 'cf-top' : ''}`} key={key}>
          <div className="cf-section-header" onClick={() => toggle(path)}>
            <span className={`cf-arrow ${!collapsed.has(path) ? 'cf-expanded' : ''}`}>▶</span>
            <span className="cf-section-title">{key}</span>
          </div>
          {!collapsed.has(path) && (
            <div className="cf-section-body">
              {entries.map(([k, v]) => renderField(k, v, path))}
            </div>
          )}
        </div>
      )
    }

    return null
  }

  function renderObject(obj: Record<string, unknown>, prefix: string): React.ReactNode {
    const entries = Object.entries(obj)
    return (
      <div className="cf-inline-object">
        {entries.map(([k, v]) => renderField(k, v, prefix))}
      </div>
    )
  }

  return (
    <div className="cf-root">
      {Object.entries(value).map(([key, val]) => renderField(key, val, ''))}
    </div>
  )
}
