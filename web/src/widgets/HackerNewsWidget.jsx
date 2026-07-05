import React, { useState } from 'react'
import { usePoll } from '../lib/usePoll'
import { api } from '../lib/api'

// HackerNewsWidget: top stories with score/comments. Read items dim (5.5-style),
// list scrolls within the fixed widget. Story count adjustable via a slider.
export default function HackerNewsWidget({ widget, onChange }) {
  const s = widget.settings
  const st = widget.state || { read: [] }
  const [showSettings, setShowSettings] = useState(false)

  const { data, error, loading } = usePoll(() => api.hn(s.limit), 5 * 60 * 1000, [s.limit])

  const read = new Set(st.read)
  const markRead = (id) => {
    if (read.has(id)) return
    onChange({ ...widget, state: { ...st, read: [...st.read, id].slice(-1000) } })
  }

  if (showSettings) {
    return (
      <div>
        <div className="section">
          <label>Stories: {s.limit}</label>
          <input type="range" min="5" max="50" step="5" value={s.limit}
            onChange={(e) => onChange({ ...widget, settings: { ...s, limit: Number(e.target.value) } })}
            style={{ width: '100%' }} />
        </div>
        <button className="btn primary" onClick={() => setShowSettings(false)}>Done</button>
      </div>
    )
  }
  if (loading && !data) return <div className="center-note">Loading Hacker News…</div>
  if (error) return <div className="center-note err-note">{error}</div>

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
        <button className="head-btn" onClick={() => setShowSettings(true)}>⚙</button>
      </div>
      <ul className="feed-list">
        {(data || []).map((it, i) => (
          <li key={it.id} className={'feed-item' + (read.has(it.id) ? ' read' : '')}>
            <span className="feed-rank">{i + 1}</span>
            <div style={{ flex: 1 }}>
              <a href={it.url} target="_blank" rel="noreferrer" onClick={() => markRead(it.id)}>{it.title}</a>
              <div className="meta">
                ▲ {it.score} · <a href={`https://news.ycombinator.com/item?id=${it.id}`} target="_blank" rel="noreferrer" style={{ fontSize: 11 }}>{it.comments} comments</a> · {it.by}
              </div>
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}
