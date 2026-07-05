import React, { useState } from 'react'
import { usePoll } from '../lib/usePoll'
import { api } from '../lib/api'

// WMO weather-code -> emoji + label. Covers the codes Open-Meteo returns.
const WMO = {
  0: ['☀️', 'Clear'], 1: ['🌤️', 'Mostly clear'], 2: ['⛅', 'Partly cloudy'], 3: ['☁️', 'Overcast'],
  45: ['🌫️', 'Fog'], 48: ['🌫️', 'Rime fog'],
  51: ['🌦️', 'Light drizzle'], 53: ['🌦️', 'Drizzle'], 55: ['🌧️', 'Dense drizzle'],
  61: ['🌦️', 'Light rain'], 63: ['🌧️', 'Rain'], 65: ['🌧️', 'Heavy rain'],
  66: ['🌧️', 'Freezing rain'], 67: ['🌧️', 'Freezing rain'],
  71: ['🌨️', 'Light snow'], 73: ['🌨️', 'Snow'], 75: ['❄️', 'Heavy snow'], 77: ['❄️', 'Snow grains'],
  80: ['🌦️', 'Rain showers'], 81: ['🌧️', 'Rain showers'], 82: ['⛈️', 'Violent showers'],
  85: ['🌨️', 'Snow showers'], 86: ['❄️', 'Snow showers'],
  95: ['⛈️', 'Thunderstorm'], 96: ['⛈️', 'Thunderstorm + hail'], 99: ['⛈️', 'Thunderstorm + hail'],
}
const wmo = (c) => WMO[c] || ['❓', '—']

const hhmm = (iso) => (iso ? new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '—')
const dayName = (d) => new Date(d).toLocaleDateString(undefined, { weekday: 'short' })
const today = () => new Date().toISOString().slice(0, 10)

export default function WeatherWidget({ widget, onChange }) {
  const s = widget.settings
  const [editing, setEditing] = useState(false)

  const { data, error, loading } = usePoll(
    () => api.weather({
      lat: s.auto ? undefined : s.lat,
      lon: s.auto ? undefined : s.lon,
      temp: s.tempUnit, wind: s.windUnit,
    }),
    15 * 60 * 1000, // refresh every 15 min
    [s.auto, s.lat, s.lon, s.tempUnit, s.windUnit],
  )

  if (editing) return <LocationEditor widget={widget} onChange={onChange} done={() => setEditing(false)} />
  if (loading && !data) return <div className="center-note">Loading weather…</div>
  if (error) return <div className="center-note err-note">Weather error: {error}</div>
  if (!data) return null

  const c = data.current
  const [icon, label] = wmo(c.weatherCode)
  const wU = data.units.wind || ''
  const todayDay = data.daily.find((d) => d.date === today()) || data.daily[0]
  // Week min/max drive the temperature range bars.
  const weekMin = Math.min(...data.daily.map((d) => d.min))
  const weekMax = Math.max(...data.daily.map((d) => d.max))
  const span = weekMax - weekMin || 1
  const place = (s.auto ? data.place : s.place) || 'Unknown location'

  // Show both units: the data comes back in the primary unit; convert to the other.
  const primU = s.tempUnit === 'fahrenheit' ? 'F' : 'C'
  const altU = primU === 'C' ? 'F' : 'C'
  const toAlt = (t) => (primU === 'C' ? t * 9 / 5 + 32 : (t - 32) * 5 / 9)

  return (
    // Uses the app's glass theme (not a coloured card) so it matches the rest;
    // the layout still follows Apple Weather.
    <div className="wx-card">
      <div className="wx-top">
        <span className="wx-place">📍 {place}</span>
        <button className="head-btn" onClick={() => setEditing(true)} title="Change location">Edit</button>
      </div>

      <div className="wx-current">
        <div className="wx-bigrow">
          <span className="wx-icon">{icon}</span>
          <span className="wx-big">{Math.round(c.temperature)}°<span className="wx-unit">{primU}</span></span>
          <span className="wx-alt">{Math.round(toAlt(c.temperature))}°{altU}</span>
        </div>
        <div className="wx-cond">{label}</div>
        <div className="wx-hl">H:{Math.round(todayDay?.max)}°&nbsp;&nbsp;L:{Math.round(todayDay?.min)}°</div>
      </div>

      {/* Hourly */}
      <div className="wx-hours">
        {data.hourly.slice(0, 12).map((hp, i) => (
          <div key={hp.time} className="wx-hour">
            <div>{i === 0 ? 'Now' : new Date(hp.time).getHours()}</div>
            <div className="h-ic">{wmo(hp.weatherCode)[0]}</div>
            <div className="h-temp">{Math.round(hp.temperature)}°</div>
            <div className="h-rain">{hp.rainChance > 0 ? `${hp.rainChance}%` : ''}</div>
          </div>
        ))}
      </div>

      {/* Daily with temperature-range bars. Past days first (past_days=2). */}
      <div className="wx-days">
        {data.daily.map((d) => {
          const isToday = d.date === today()
          const past = d.date < today()
          const left = ((d.min - weekMin) / span) * 100
          const width = Math.max(8, ((d.max - d.min) / span) * 100)
          return (
            <div className="wx-day" key={d.date} style={past ? { opacity: 0.5 } : null}>
              <span className="d-name">{isToday ? 'Today' : dayName(d.date)}</span>
              <span className="d-icon">{wmo(d.weatherCode)[0]}</span>
              <span className="d-lo">{Math.round(d.min)}°</span>
              <span className="wx-range">
                <span className="wx-range-fill" style={{ left: `${left}%`, width: `${width}%` }} />
              </span>
              <span className="d-hi">{Math.round(d.max)}°</span>
            </div>
          )
        })}
      </div>

      {/* Metric tiles (Apple Weather bottom grid) */}
      <div className="wx-tiles">
        <Tile label="Feels" value={`${Math.round(c.apparentTemperature)}°`} />
        <Tile label="Wind" value={`${Math.round(c.windSpeed)} ${wU}`} />
        <Tile label="Humidity" value={`${c.humidity}%`} />
        <Tile label="Rain" value={`${todayDay?.rainChance ?? 0}%`} />
        <Tile label="Sunrise" value={hhmm(todayDay?.sunrise)} />
        <Tile label="Sunset" value={hhmm(todayDay?.sunset)} />
      </div>
    </div>
  )
}

// Tile is one frosted metric cell at the bottom of the weather card.
function Tile({ label, value }) {
  return (
    <div className="wx-tile">
      <div className="t-label">{label}</div>
      <div className="t-value">{value}</div>
    </div>
  )
}


// LocationEditor: auto-locate toggle + place search (Open-Meteo geocoding).
function LocationEditor({ widget, onChange, done }) {
  const s = widget.settings
  const [q, setQ] = useState(s.place || '')
  const [results, setResults] = useState([])
  const [busy, setBusy] = useState(false)

  const search = async () => {
    if (!q.trim()) return
    setBusy(true)
    try { setResults(await api.geoSearch(q)) } catch { setResults([]) } finally { setBusy(false) }
  }
  const pick = (r) => {
    onChange({ ...widget, settings: { ...s, auto: false, lat: r.latitude, lon: r.longitude, place: [r.city, r.region, r.country].filter(Boolean).join(', ') } })
    done()
  }
  const useAuto = () => {
    onChange({ ...widget, settings: { ...s, auto: true, place: '' } })
    done()
  }
  const setUnits = (patch) => onChange({ ...widget, settings: { ...s, ...patch } })

  return (
    <div>
      <div className="section">
        <button className="btn" onClick={useAuto}>📍 Use my location (auto)</button>
      </div>
      <div className="section">
        <label>Search a place</label>
        <div className="inline-add">
          <input value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && search()} placeholder="e.g. Tokyo" />
          <button className="btn" onClick={search}>{busy ? '…' : 'Search'}</button>
        </div>
        {results.map((r, i) => (
          <div key={i} className="chip active" style={{ marginTop: 6 }} onClick={() => pick(r)}>
            {[r.city, r.region, r.country].filter(Boolean).join(', ')}
          </div>
        ))}
      </div>
      <div className="section">
        <label>Units</label>
        <div className="chips">
          <button className={'chip' + (s.tempUnit === 'celsius' ? ' active' : '')} onClick={() => setUnits({ tempUnit: 'celsius' })}>°C</button>
          <button className={'chip' + (s.tempUnit === 'fahrenheit' ? ' active' : '')} onClick={() => setUnits({ tempUnit: 'fahrenheit' })}>°F</button>
          <button className={'chip' + (s.windUnit === 'kmh' ? ' active' : '')} onClick={() => setUnits({ windUnit: 'kmh' })}>km/h</button>
          <button className={'chip' + (s.windUnit === 'mph' ? ' active' : '')} onClick={() => setUnits({ windUnit: 'mph' })}>mph</button>
        </div>
      </div>
      <button className="btn primary" onClick={done}>Done</button>
    </div>
  )
}
