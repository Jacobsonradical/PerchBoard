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

  // Optional LLM key for smart RSS filtering. The key itself is never returned by
  // the server, so we only track provider/model and whether one is configured.
  const [llm, setLlm] = useState({ configured: false, provider: 'claude', model: '' })
  const [llmKey, setLlmKey] = useState('')
  const [llmBusy, setLlmBusy] = useState(false)
  const [llmMsg, setLlmMsg] = useState('')
  const [llmConsent, setLlmConsent] = useState(false)
  const [llmEditing, setLlmEditing] = useState(false) // showing the key-entry form
  const refreshLlm = () =>
    api.llmConfig()
      .then((c) => setLlm({ configured: !!c.configured, provider: c.provider || 'claude', model: c.model || '' }))
      .catch(() => {})

  // Load the list of uploaded backgrounds + any saved LLM config.
  const refresh = () => api.listBackgrounds().then(setFiles).catch(() => setFiles([]))
  useEffect(() => { refresh(); refreshLlm() }, [])

  const saveLlm = async () => {
    // Entering/replacing a key requires the acknowledgement (changing only the
    // model with a key already saved does not).
    if (llmKey && !llmConsent) {
      setLlmMsg('Please tick the acknowledgement to save a key.')
      return
    }
    setLlmBusy(true); setLlmMsg('')
    try {
      await api.llmSaveConfig({ provider: llm.provider, model: llm.model, key: llmKey })
      setLlmKey('')
      setLlmConsent(false)
      setLlmEditing(false)
      await refreshLlm()
      setLlmMsg('Saved.')
    } catch (e) {
      setLlmMsg(e.message || 'Save failed.')
    } finally {
      setLlmBusy(false)
    }
  }
  const forgetLlm = async () => {
    await api.llmClearConfig()
    setLlmKey('')
    setLlmConsent(false)
    setLlmEditing(false)
    await refreshLlm()
    setLlmMsg('API key removed.')
  }
  const testLlm = async () => {
    setLlmBusy(true); setLlmMsg('Testing…')
    try {
      const r = await api.llmTest()
      setLlmMsg(`✅ Connected (${r.provider || 'llm'}${r.model ? ' · ' + r.model : ''})`)
    } catch (e) {
      setLlmMsg('❌ ' + (e.message || 'connection failed'))
    } finally {
      setLlmBusy(false)
    }
  }

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

        <div className="section">
          <label>AI smart filtering (optional)</label>
          <div className="muted-note" style={{ marginBottom: 8 }}>
            Add an API key to let RSS widgets sort items into your filter groups by
            meaning (turn it on per widget under the RSS ⚙). The key is stored only on
            this device and used only to classify feed titles.
          </div>

          {llm.configured && !llmEditing ? (
            // --- a key is saved: compact status, no form ---
            <div className="s1-save-panel">
              <div className="s1-lock-note">
                🔒 {llm.provider === 'openai' ? 'OpenAI' : 'Claude'} key saved
                {llm.model ? ` · ${llm.model}` : ''}. Turn smart filtering on per RSS widget.
              </div>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                <button className="btn" onClick={testLlm} disabled={llmBusy}>Test connection</button>
                <button className="btn" onClick={() => { setLlmMsg(''); setLlmEditing(true) }}>Change key</button>
                <button className="btn" onClick={forgetLlm}>Forget key</button>
                {llmMsg && <span className="muted-note">{llmMsg}</span>}
              </div>
            </div>
          ) : (
            // --- no key yet, or changing it: the setup form ---
            <>
              <div className="chips" style={{ marginBottom: 8 }}>
                {['claude', 'openai'].map((p) => (
                  <button key={p}
                    className={'chip' + (llm.provider === p ? ' active' : '')}
                    onClick={() => setLlm({ ...llm, provider: p })}>
                    {p === 'claude' ? 'Claude' : 'OpenAI'}
                  </button>
                ))}
              </div>
              <input
                className="text-input"
                placeholder={llm.provider === 'openai' ? 'Model (e.g. gpt-4o-mini)' : 'Model (e.g. claude-haiku-4-5)'}
                value={llm.model}
                onChange={(e) => setLlm({ ...llm, model: e.target.value })}
                style={{ marginBottom: 8 }}
              />
              <input
                className="text-input"
                type="password"
                autoComplete="new-password"
                placeholder="API key"
                value={llmKey}
                onChange={(e) => setLlmKey(e.target.value)}
                style={{ marginBottom: 8 }}
              />
              <div className="s1-warn" style={{ marginBottom: 8 }}>
                ⚠ Classifying feeds makes billable calls to your account. Create a
                <strong> dedicated key just for PerchBoard</strong> — e.g. a new Project in the
                OpenAI portal, or a scoped Anthropic key — and set a <strong>monthly spend
                limit</strong> (for example $50). The key is stored only on this device, but no
                storage is perfectly safe: PerchBoard is <strong>not responsible</strong> for any
                loss, leak, misuse, or charges arising from your API key.
              </div>
              <label className="s1-check" style={{ marginBottom: 8 }}>
                <input type="checkbox" checked={llmConsent} onChange={(e) => setLlmConsent(e.target.checked)} />
                I understand and accept the risk, and I've set a spend limit on a dedicated key
              </label>
              <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
                <button className="btn primary" onClick={saveLlm} disabled={llmBusy}>Save</button>
                {llm.configured && (
                  <button className="btn" onClick={() => { setLlmEditing(false); setLlmKey(''); setLlmConsent(false); setLlmMsg('') }}>
                    Cancel
                  </button>
                )}
                {llmMsg && <span className="muted-note">{llmMsg}</span>}
              </div>
            </>
          )}
        </div>

        <div style={{ textAlign: 'right', marginTop: 16 }}>
          <button className="btn primary" onClick={onClose}>Done</button>
        </div>
      </div>
    </div>
  )
}
