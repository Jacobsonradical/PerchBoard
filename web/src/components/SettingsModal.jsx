import React, { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { requestNotifyPermission } from '../lib/notify'
import { FONTS, FONT_SCALES } from '../lib/fonts'

// SettingsModal handles global dashboard settings: fonts, background images
// (upload, choose mode, shuffle interval) and enabling OS notifications.
export default function SettingsModal({ settings, onChange, onClose }) {
  const [files, setFiles] = useState([])      // available image filenames on server
  const [busy, setBusy] = useState(false)
  const [notifyOn, setNotifyOn] = useState(
    typeof Notification !== 'undefined' && Notification.permission === 'granted',
  )

  // Load the list of uploaded backgrounds.
  const refresh = () => api.listBackgrounds().then(setFiles).catch(() => setFiles([]))
  useEffect(() => { refresh() }, [])

  const set = (patch) => onChange({ ...settings, ...patch })

  const upload = async (e) => {
    const list = Array.from(e.target.files || [])
    if (list.length === 0) return
    setBusy(true)
    try {
      const uploaded = []
      for (const f of list) {
        const { name } = await api.uploadBackground(f)
        uploaded.push(name)
      }
      await refresh()
      // Auto-enable backgrounds and select the newly uploaded ones.
      const next = Array.from(new Set([...(settings.backgrounds || []), ...uploaded]))
      set({ backgrounds: next, backgroundMode: settings.backgroundMode === 'none' ? 'shuffle' : settings.backgroundMode })
    } finally {
      setBusy(false)
      e.target.value = ''
    }
  }

  const toggleSelected = (name) => {
    const sel = new Set(settings.backgrounds || [])
    sel.has(name) ? sel.delete(name) : sel.add(name)
    set({ backgrounds: Array.from(sel) })
  }

  const remove = async (name) => {
    await api.deleteBackground(name)
    await refresh()
    set({ backgrounds: (settings.backgrounds || []).filter((n) => n !== name) })
  }

  const enableNotify = async () => setNotifyOn(await requestNotifyPermission())

  const selected = new Set(settings.backgrounds || [])

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h2>Settings</h2>

        <div className="section">
          <label>Dashboard name</label>
          <input
            type="text"
            className="text-input"
            value={settings.brandName ?? ''}
            placeholder="PerchBoard"
            onChange={(e) => set({ brandName: e.target.value })}
          />
        </div>

        <div className="section">
          <label>Font</label>
          <div className="chips">
            {Object.entries(FONTS).map(([key, f]) => (
              <button key={key}
                className={'chip' + (settings.fontFamily === key ? ' active' : '')}
                style={{ fontFamily: f.stack }}
                onClick={() => set({ fontFamily: key })}>{f.label}</button>
            ))}
          </div>
        </div>

        <div className="section">
          <label>Text size</label>
          <div className="chips">
            {FONT_SCALES.map((s) => (
              <button key={s.label}
                className={'chip' + ((settings.fontScale || 1) === s.value ? ' active' : '')}
                onClick={() => set({ fontScale: s.value })}>{s.label}</button>
            ))}
          </div>
        </div>

        <div className="section">
          <label>Background</label>
          <div className="chips">
            {['none', 'static', 'shuffle'].map((m) => (
              <button key={m} className={'chip' + (settings.backgroundMode === m ? ' active' : '')} onClick={() => set({ backgroundMode: m })}>{m}</button>
            ))}
          </div>
        </div>

        {settings.backgroundMode === 'shuffle' && (
          <div className="section">
            <label>Shuffle every {settings.shuffleSeconds}s</label>
            <input type="range" min="10" max="600" step="10" value={settings.shuffleSeconds}
              onChange={(e) => set({ shuffleSeconds: Number(e.target.value) })} style={{ width: '100%' }} />
          </div>
        )}

        <div className="section">
          <label>Images (click to use; selected get a coloured border)</label>
          <div className="bg-thumbs">
            {files.map((name) => (
              <div key={name}
                className={'bg-thumb' + (selected.has(name) ? ' active' : '')}
                style={{ backgroundImage: `url("/backgrounds/${name}")` }}
                onClick={() => toggleSelected(name)}>
                <button onClick={(e) => { e.stopPropagation(); remove(name) }} title="Delete">✕</button>
              </div>
            ))}
            {files.length === 0 && <span className="muted-note">No images uploaded yet.</span>}
          </div>
          <div className="inline-add" style={{ marginTop: 10 }}>
            <input type="file" accept="image/*" multiple onChange={upload} disabled={busy} />
            {busy && <span className="muted-note">uploading…</span>}
          </div>
        </div>

        <div className="section">
          <label>Notifications</label>
          {notifyOn
            ? <span className="muted-note">✅ OS notifications enabled</span>
            : <button className="btn" onClick={enableNotify}>Enable OS notifications</button>}
        </div>

        <div style={{ textAlign: 'right', marginTop: 16 }}>
          <button className="btn primary" onClick={onClose}>Done</button>
        </div>
      </div>
    </div>
  )
}
