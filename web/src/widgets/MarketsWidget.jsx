import React, { useState, useRef } from 'react'
import { usePoll } from '../lib/usePoll'
import { api } from '../lib/api'

// MarketsWidget: a tabbed stock browser. Tabs along the bottom (one per symbol,
// with its % change); selecting one shows the company name, price, an
// interactive intraday chart (hover for price + time), key stats, and news
// filtered to that company. Add symbols by searching a name (e.g. "Nvidia").
export default function MarketsWidget({ widget, onChange }) {
  const s = widget.settings
  const [showSettings, setShowSettings] = useState(s.symbols.length === 0)
  const [active, setActive] = useState(s.symbols[0] || '')

  const { data, error, loading } = usePoll(() => api.markets(s.symbols), 60 * 1000, [s.symbols.join(',')])

  const quotes = data || []
  const activeSym = quotes.some((q) => q.symbol === active) ? active : quotes[0]?.symbol
  const q = quotes.find((x) => x.symbol === activeSym)

  const news = usePoll(
    () => (activeSym ? api.stockNews(activeSym) : Promise.resolve([])),
    10 * 60 * 1000,
    [activeSym],
  )

  if (showSettings) return <MarketsSettings widget={widget} onChange={onChange} done={() => setShowSettings(false)} />
  if (loading && !data) return <div className="center-note">Loading markets…</div>
  if (error) return <div className="center-note err-note">{error}</div>

  return (
    <div className="mkt">
      <div className="mkt-detail">
        {q ? <Detail q={q} news={news.data} /> : <div className="center-note">No symbols.</div>}
      </div>

      <div className="mkt-tabs">
        {quotes.map((x) => (
          <button key={x.symbol}
            className={'mkt-tab' + (x.symbol === activeSym ? ' active' : '')}
            onClick={() => setActive(x.symbol)} title={x.name || x.symbol}>
            <span className="tab-sym">{x.symbol}</span>
            <span className={'tab-chg ' + (x.change >= 0 ? 'up' : 'down')}>
              {x.change >= 0 ? '▲' : '▼'} {Math.abs(x.changePercent).toFixed(1)}%
            </span>
          </button>
        ))}
        <button className="head-btn mkt-manage" onClick={() => setShowSettings(true)} title="Manage symbols">＋⚙</button>
      </div>
    </div>
  )
}

function Detail({ q, news }) {
  if (q.error) return <div className="center-note err-note">{q.symbol}: {q.error}</div>
  const up = q.change >= 0

  // Keep only news that actually mentions the company or ticker (Yahoo's feed
  // can include unrelated market stories).
  const nameKey = (q.name || '').split(/[\s,.]+/)[0].toLowerCase()
  const relevant = (news || []).filter((n) => {
    const t = (n.title || '').toLowerCase()
    return t.includes(q.symbol.toLowerCase()) || (nameKey.length >= 3 && t.includes(nameKey))
  })

  return (
    <>
      <div className="mkt-d-head">
        <div className="mkt-d-id">
          <div className="mkt-d-sym">{q.symbol}</div>
          <div className="mkt-d-name">{q.name || q.currency}</div>
        </div>
        <div style={{ textAlign: 'right' }}>
          <div className="mkt-d-price">{fmt(q.price)}</div>
          <div className={'mkt-d-chg ' + (up ? 'up' : 'down')}>
            {up ? '+' : ''}{fmt(q.change)} ({q.changePercent.toFixed(2)}%)
          </div>
        </div>
      </div>

      <BigSpark data={q.spark} times={q.sparkTimes} up={up} />

      <div className="mkt-stats">
        <Stat label="Prev Close" v={fmt(q.previousClose)} />
        <Stat label="Day High" v={fmt(q.dayHigh)} />
        <Stat label="Day Low" v={fmt(q.dayLow)} />
        <Stat label="52W High" v={fmt(q.week52High)} />
        <Stat label="52W Low" v={fmt(q.week52Low)} />
        <Stat label="Volume" v={fmtVol(q.volume)} />
      </div>

      <div className="mkt-news-title">Related news</div>
      {relevant.length > 0
        ? relevant.map((n, i) => (
            <a key={i} className="mkt-news-item" href={n.link} target="_blank" rel="noreferrer">
              <div className="n-title">{n.title}</div>
              <div className="n-meta">{n.publisher} · {timeAgo(n.time)}</div>
            </a>
          ))
        : <div className="muted-note">No recent news about {q.name || q.symbol}.</div>}
    </>
  )
}

const Stat = ({ label, v }) => (
  <div className="mkt-stat"><div className="s-label">{label}</div><div className="s-val">{v}</div></div>
)

// BigSpark: full-width intraday chart. Hovering shows a crosshair plus a tooltip
// with the price (and time) at that point.
function BigSpark({ data, times, up }) {
  const ref = useRef(null)
  const [hover, setHover] = useState(null)
  if (!data || data.length < 2) return <div className="muted-note" style={{ margin: '10px 0' }}>No intraday data.</div>

  const w = 300, h = 70
  const min = Math.min(...data), max = Math.max(...data)
  const range = max - min || 1
  const xOf = (i) => (i / (data.length - 1)) * w
  const yOf = (v) => h - ((v - min) / range) * h
  const line = data.map((v, i) => `${xOf(i)},${yOf(v)}`).join(' ')
  const area = `0,${h} ${line} ${w},${h}`
  const color = up ? '#4ade80' : '#f87171'

  const onMove = (e) => {
    const rect = ref.current.getBoundingClientRect()
    const frac = (e.clientX - rect.left) / rect.width
    setHover(Math.max(0, Math.min(data.length - 1, Math.round(frac * (data.length - 1)))))
  }

  return (
    <div className="mkt-chartwrap">
      <svg ref={ref} className="mkt-bigspark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none"
        onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
        <polygon points={area} fill={color} opacity="0.12" />
        <polyline points={line} fill="none" stroke={color} strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
        {hover != null && (
          <>
            <line x1={xOf(hover)} y1="0" x2={xOf(hover)} y2={h} stroke="var(--fg-dim)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
            <circle cx={xOf(hover)} cy={yOf(data[hover])} r="3" fill={color} vectorEffect="non-scaling-stroke" />
          </>
        )}
      </svg>
      {hover != null && (
        <div className="mkt-tip" style={{ left: `${(hover / (data.length - 1)) * 100}%` }}>
          <span className="tip-price">{fmt(data[hover])}</span>
          {times && times[hover] ? <span className="tip-time">{hhmm(times[hover])}</span> : null}
        </div>
      )}
    </div>
  )
}

const fmt = (n) =>
  n == null ? '—' : n >= 1000 ? n.toLocaleString(undefined, { maximumFractionDigits: 0 }) : n.toFixed(2)
const fmtVol = (n) => {
  if (!n) return '—'
  if (n >= 1e9) return (n / 1e9).toFixed(1) + 'B'
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(n)
}
const hhmm = (unix) => new Date(unix * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
const timeAgo = (unix) => {
  if (!unix) return ''
  const diff = Date.now() / 1000 - unix
  if (diff < 3600) return `${Math.max(1, Math.round(diff / 60))}m`
  if (diff < 86400) return `${Math.round(diff / 3600)}h`
  return `${Math.round(diff / 86400)}d`
}

// MarketsSettings: search a company/ticker to add (e.g. "Nvidia" -> NVDA).
function MarketsSettings({ widget, onChange, done }) {
  const s = widget.settings
  const [q, setQ] = useState('')
  const [results, setResults] = useState([])
  const [busy, setBusy] = useState(false)
  const set = (symbols) => onChange({ ...widget, settings: { ...s, symbols } })

  const search = async () => {
    if (!q.trim()) return
    setBusy(true)
    try { setResults(await api.symbolSearch(q)) } catch { setResults([]) } finally { setBusy(false) }
  }
  const pick = (sym) => {
    if (sym && !s.symbols.includes(sym)) set([...s.symbols, sym])
    setResults([]); setQ('')
  }

  return (
    <div>
      <div className="section">
        <label>Your symbols</label>
        <div className="chips">
          {s.symbols.map((x) => (
            <span className="chip" key={x}>{x}<button onClick={() => set(s.symbols.filter((y) => y !== x))}>✕</button></span>
          ))}
          {s.symbols.length === 0 && <span className="muted-note">none yet</span>}
        </div>
      </div>

      <div className="section">
        <label>Add by name or ticker</label>
        <div className="inline-add">
          <input value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && search()} placeholder="e.g. Nvidia, BTC, Apple" />
          <button className="btn primary" onClick={search}>{busy ? '…' : 'Search'}</button>
        </div>
        {results.map((r) => (
          <div key={r.symbol} className="sym-result" onClick={() => pick(r.symbol)}>
            <span className="sym-code">{r.symbol}</span>
            <span className="sym-name">{r.name}</span>
            <span className="sym-exch">{r.exchange}</span>
          </div>
        ))}
      </div>

      <button className="btn primary" onClick={done}>Done</button>
    </div>
  )
}
