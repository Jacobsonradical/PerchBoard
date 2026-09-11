import React, { useEffect, useState } from 'react'

async function summaryConfig(method = 'GET', body) {
  const response = await fetch('/api/summary/config', {
    method,
    ...(body && { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  })
  const data = await response.json()
  if (!response.ok) throw new Error(data.error || 'Could not update summary settings.')
  return data
}

export default function SummarySettings() {
  const [saved, setSaved] = useState(null)
  const [provider, setProvider] = useState('claude')
  const [model, setModel] = useState('')
  const [key, setKey] = useState('')
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')

  const accept = (data) => {
    setSaved(data); setProvider(data.provider || 'claude'); setModel(data.model || '')
    setKey(''); setEditing(false)
  }
  useEffect(() => {
    let active = true
    summaryConfig().then((data) => { if (active) accept(data) })
      .catch((error) => { if (active) setMessage(error.message) })
    return () => { active = false }
  }, [])

  const update = async (method) => {
    setBusy(true); setMessage('')
    try {
      accept(await summaryConfig(method, method === 'POST' ? { provider, model, key } : undefined))
      setMessage(method === 'DELETE' ? 'Summary key removed.' : 'Summary settings saved.')
    } catch (error) { setMessage(error.message) }
    finally { setBusy(false) }
  }

  return <div className="section summary-settings">
    <label>AI article summary (optional)</label>
    <p className="muted-note">Get two or three sentences about what happened, who is involved, and the main outcome, in the article’s language. Enable AI summary separately for each RSS feed.</p>
    <p className="muted-note">Uses its own model and API key, independent of smart filtering. Opening an enabled article sends its extracted text to your selected provider and may incur API charges. The key stays on the PerchBoard server after saving.</p>
    {saved?.configured && !editing ? <div className="s1-save-panel">
      <div className="s1-lock-note">{saved.provider === 'openai' ? 'OpenAI / GPT' : 'Claude'} · {saved.model} · Summary key saved</div>
      <div className="summary-settings-actions">
        <button className="btn" disabled={busy} onClick={() => { setEditing(true); setMessage('') }}>Edit summary settings</button>
        <button className="btn" disabled={busy} onClick={() => update('DELETE')}>Forget summary key</button>
      </div>
    </div> : saved && <fieldset disabled={busy} className="summary-settings-fields">
      <div className="chips">
        {['claude', 'openai'].map((value) => <button key={value} className={'chip' + (provider === value ? ' active' : '')}
          onClick={() => { setProvider(value); setModel(''); setKey('') }}>{value === 'openai' ? 'OpenAI / GPT' : 'Claude'}</button>)}
      </div>
      <label htmlFor="summary-model">Summary model ID</label>
      <input id="summary-model" className="text-input" value={model} onChange={(event) => setModel(event.target.value)} placeholder="Enter your provider’s model ID" autoComplete="off" />
      <label htmlFor="summary-key">Summary API key</label>
      <input id="summary-key" className="text-input" type="password" autoComplete="new-password" value={key} onChange={(event) => setKey(event.target.value)}
        placeholder={saved.configured && saved.provider === provider ? 'Leave blank to keep the saved summary key' : 'Enter a separate summary API key'} />
      <div className="summary-settings-actions">
        <button className="btn primary" disabled={!model.trim() || (!key.trim() && !(saved.configured && saved.provider === provider))} onClick={() => update('POST')}>Save summary settings</button>
        {saved.configured && <button className="btn" onClick={() => { accept(saved); setMessage('') }}>Cancel</button>}
      </div>
    </fieldset>}
    <div className="muted-note" role="status">{message || (!saved ? 'Loading summary settings…' : '')}</div>
  </div>
}
