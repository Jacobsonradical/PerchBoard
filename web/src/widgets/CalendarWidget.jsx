import React, { useState } from 'react'

// CalendarWidget: a clean month view with today highlighted and month
// navigation. Prototype scope is a local calendar; Google Calendar sync is a
// planned follow-up (see record file) and would plug in here as event dots.
const DOW = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa']

export default function CalendarWidget() {
  // `view` is the first day of the month currently shown.
  const [view, setView] = useState(() => {
    const n = new Date()
    return new Date(n.getFullYear(), n.getMonth(), 1)
  })
  // The day the user has clicked (null = none). Lets you pick any date.
  const [selected, setSelected] = useState(null)

  const year = view.getFullYear()
  const month = view.getMonth()
  const firstDow = new Date(year, month, 1).getDay()
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const todayStr = new Date().toDateString()

  // Build a 6x7 grid of cells, including trailing days from adjacent months.
  const cells = []
  for (let i = 0; i < firstDow; i++) cells.push(null)
  for (let d = 1; d <= daysInMonth; d++) cells.push(new Date(year, month, d))

  const shift = (delta) => setView(new Date(year, month + delta, 1))
  const monthLabel = view.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })

  return (
    <div>
      <div className="cal-head">
        <span className="cal-title">{monthLabel}</span>
        <button className="head-btn" onClick={() => shift(-1)}>‹</button>
        <button className="head-btn" onClick={() => setView(new Date(new Date().getFullYear(), new Date().getMonth(), 1))}>•</button>
        <button className="head-btn" onClick={() => shift(1)}>›</button>
      </div>
      <div className="cal-grid">
        {DOW.map((d) => <div key={d} className="cal-dow">{d}</div>)}
        {cells.map((c, i) => {
          const isToday = c && c.toDateString() === todayStr
          const isSel = c && selected && c.toDateString() === selected.toDateString()
          return (
            <div key={i}
              className={'cal-cell' + (isToday ? ' today' : '') + (isSel ? ' selected' : '') + (!c ? ' muted' : '')}
              onClick={() => c && setSelected(c)}>
              {c ? <span className="cal-num">{c.getDate()}</span> : ''}
            </div>
          )
        })}
      </div>

      {/* Selected day readout (foundation for future events). */}
      {selected && (
        <div className="cal-selected">
          {selected.toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' })}
        </div>
      )}
    </div>
  )
}
