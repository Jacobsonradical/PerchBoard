import React, { useEffect, useState, useCallback } from 'react'
import Dashboard from './components/Dashboard'
import Background from './components/Background'
import AddWidgetModal from './components/AddWidgetModal'
import SettingsModal from './components/SettingsModal'
import { loadConfig, saveConfig, newId, widgetDefaults, widgetSize } from './lib/store'
import { onToast } from './lib/notify'
import { applyFont } from './lib/fonts'

export default function App() {
  const [config, setConfig] = useState(null)   // null until loaded
  const [editMode, setEditMode] = useState(false)
  const [showAdd, setShowAdd] = useState(false)
  const [showSettings, setShowSettings] = useState(false)
  const [toasts, setToasts] = useState([])

  // Load persisted dashboard once.
  useEffect(() => { loadConfig().then(setConfig) }, [])

  // Apply font family + scale whenever they change.
  useEffect(() => {
    if (config) applyFont(config.settings.fontFamily, config.settings.fontScale)
  }, [config?.settings.fontFamily, config?.settings.fontScale])

  // Subscribe to in-app toasts; auto-dismiss after 6s.
  useEffect(() => onToast((t) => {
    setToasts((cur) => [...cur, t])
    setTimeout(() => setToasts((cur) => cur.filter((x) => x.id !== t.id)), 6000)
  }), [])

  // Any config mutation updates state and schedules a debounced save.
  const update = useCallback((next) => {
    setConfig(next)
    saveConfig(next)
  }, [])

  // --- widget operations ---
  const onLayoutChange = (layout) => {
    if (!config) return
    const clean = layout.map(({ i, x, y, w, h }) => ({ i, x, y, w, h }))
    update({ ...config, layout: clean })
  }
  const onWidgetChange = (updated) =>
    update({ ...config, widgets: config.widgets.map((w) => (w.id === updated.id ? updated : w)) })

  const onWidgetRemove = (id) =>
    update({
      ...config,
      widgets: config.widgets.filter((w) => w.id !== id),
      layout: config.layout.filter((l) => l.i !== id),
    })

  const addWidget = (type) => {
    const id = newId()
    const widget = { id, type, ...structuredClone(widgetDefaults[type]) }
    // Place the new widget below everything else.
    const maxY = config.layout.reduce((m, l) => Math.max(m, l.y + l.h), 0)
    const size = widgetSize[type]
    update({
      ...config,
      widgets: [...config.widgets, widget],
      layout: [...config.layout, { i: id, x: 0, y: maxY, ...size }],
    })
    setShowAdd(false)
    setEditMode(true) // drop into edit mode so the user can place it
  }

  const onSettingsChange = (settings) => update({ ...config, settings })

  if (!config) return <div className="center-note" style={{ height: '100vh' }}>Loading PerchBoard…</div>

  return (
    <>
      <Background settings={config.settings} />

      <div className="toolbar">
        <div className="toolbar-inner">
          <span className="brand"><img className="brand-logo" src="/icon.png" alt="" /> {config.settings.brandName || 'PerchBoard'}</span>
          <button className="btn" onClick={() => setShowAdd(true)}>＋ Add widget</button>
          <button className={'btn' + (editMode ? ' on' : '')} onClick={() => setEditMode((e) => !e)}>
            {editMode ? '✓ Done arranging' : '✥ Arrange'}
          </button>
          <button className="btn icon" title="Settings" onClick={() => setShowSettings(true)}>⚙</button>
        </div>
      </div>

      <Dashboard
        config={config}
        editMode={editMode}
        onLayoutChange={onLayoutChange}
        onWidgetChange={onWidgetChange}
        onWidgetRemove={onWidgetRemove}
      />

      {showAdd && <AddWidgetModal onPick={addWidget} onClose={() => setShowAdd(false)} />}
      {showSettings && (
        <SettingsModal settings={config.settings} onChange={onSettingsChange} onClose={() => setShowSettings(false)} />
      )}

      {/* Toasts (pretty notifications) */}
      <div className="toasts">
        {toasts.map((t) => (
          <div key={t.id} className="toast">
            <div className="t-title">{t.title}</div>
            {t.body && <div className="t-body">{t.body}</div>}
          </div>
        ))}
      </div>
    </>
  )
}
