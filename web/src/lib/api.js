// Thin wrappers around the Go backend's JSON API. Every call returns parsed
// JSON or throws an Error with the backend's message (handlers return
// {"error": "..."} on failure), so widgets can show a clean message.

async function getJSON(url) {
  const res = await fetch(url)
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `request failed (${res.status})`)
  return data
}

export const api = {
  rss: (url) => getJSON(`/api/rss?url=${encodeURIComponent(url)}`),
  hn: (limit = 30) => getJSON(`/api/hn?limit=${limit}`),
  weather: ({ lat, lon, temp = 'celsius', wind = 'kmh' } = {}) => {
    const q = new URLSearchParams({ temp, wind })
    if (lat != null && lon != null) { q.set('lat', lat); q.set('lon', lon) }
    return getJSON(`/api/weather?${q.toString()}`)
  },
  geo: () => getJSON('/api/geo'),
  geoSearch: (q) => getJSON(`/api/geo/search?q=${encodeURIComponent(q)}`),
  markets: (symbols) => getJSON(`/api/markets?symbols=${encodeURIComponent(symbols.join(','))}`),
  stockNews: (symbol) => getJSON(`/api/stocknews?symbol=${encodeURIComponent(symbol)}`),
  symbolSearch: (q) => getJSON(`/api/symbolsearch?q=${encodeURIComponent(q)}`),

  // --- paper tracker on-demand retrieval (ScholarOne, PCS, ...) ---
  // sites: [{ key, name, url, system, username, password }]. Credentials are sent
  // once, used to fill the login form on the local server, and never stored.
  tracker: async (sites) => {
    const res = await fetch('/api/tracker/retrieve', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sites }),
    })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(data.error || `retrieve failed (${res.status})`)
    return data.results || []
  },

  // --- optional LLM key config + smart RSS filtering ---
  // The key is stored server-side; llmConfig() never returns it, only whether one
  // is set and which provider/model.
  llmConfig: () => getJSON('/api/llm/config'),
  llmSaveConfig: async ({ provider, model, key }) => {
    const res = await fetch('/api/llm/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ provider, model, key }),
    })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(data.error || 'failed to save LLM settings')
    return data
  },
  llmClearConfig: () => fetch('/api/llm/config', { method: 'DELETE' }),
  llmTest: async () => {
    const res = await fetch('/api/llm/test', { method: 'POST' })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(data.error || `test failed (${res.status})`)
    return data
  },
  llmClassify: async (items, groups) => {
    const res = await fetch('/api/llm/classify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ items, groups }),
    })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(data.error || `classify failed (${res.status})`)
    return data.classifications || {}
  },

  // --- dashboard config ---
  loadConfig: () => getJSON('/api/config'),
  saveConfig: async (cfg) => {
    const res = await fetch('/api/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(cfg),
    })
    if (!res.ok) throw new Error('failed to save config')
    return res.json()
  },

  // --- backgrounds ---
  listBackgrounds: () => getJSON('/api/backgrounds'),
  uploadBackground: async (file) => {
    const fd = new FormData()
    fd.append('image', file)
    const res = await fetch('/api/backgrounds', { method: 'POST', body: fd })
    const data = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(data.error || 'upload failed')
    return data
  },
  deleteBackground: (name) =>
    fetch(`/api/backgrounds?name=${encodeURIComponent(name)}`, { method: 'DELETE' }),
}
