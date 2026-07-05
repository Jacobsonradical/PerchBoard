import React, { useEffect, useState, useMemo } from 'react'

// ClockWidget — a fixed analog clock with hour numbers, rendered as a 3D
// celestial dial that floats in space: the disc itself is the starfield (stars
// live on the dial, not behind it). A live digital time sits below.
export default function ClockWidget() {
  const [now, setNow] = useState(() => new Date())

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(id)
  }, [])

  // Stars scattered *inside* the dial (polar sampling keeps them within the
  // circle). Generated once so they don't jump on every tick.
  const stars = useMemo(
    () =>
      Array.from({ length: 70 }, (_, i) => {
        const ang = Math.random() * Math.PI * 2
        const rad = Math.sqrt(Math.random()) * 46 // % of radius, <=46 stays inside
        return {
          id: i,
          top: 50 + rad * Math.sin(ang),
          left: 50 + rad * Math.cos(ang),
          size: 0.5 + Math.random() * 1.9,
          delay: Math.random() * 4,
          bright: 0.3 + Math.random() * 0.7,
        }
      }),
    [],
  )

  const h = now.getHours(), m = now.getMinutes(), sec = now.getSeconds()
  const pad = (n) => String(n).padStart(2, '0')

  const secDeg = sec * 6
  const minDeg = m * 6 + sec * 0.1
  const hourDeg = (h % 12) * 30 + m * 0.5
  const hand = (deg) => ({ transform: `translateX(-50%) rotate(${deg}deg)` })

  return (
    <div className="clock-celestial">
      <div className="clock-stage">
        <div className="celestial-dial">
          {/* starfield ON the dial */}
          <div className="dial-stars">
            {stars.map((s) => (
              <span key={s.id} className="star" style={{
                top: `${s.top}%`, left: `${s.left}%`,
                width: s.size, height: s.size, opacity: s.bright,
                animationDelay: `${s.delay}s`,
              }} />
            ))}
          </div>

          {/* hour numbers 1..12 */}
          {Array.from({ length: 12 }, (_, i) => i + 1).map((n) => (
            <span key={n} className="clock-num" style={{
              transform: `translate(-50%, -50%) rotate(${n * 30}deg) translateY(-64px) rotate(${-n * 30}deg)`,
            }}>{n}</span>
          ))}

          <div className="hand hour" style={hand(hourDeg)} />
          <div className="hand minute" style={hand(minDeg)} />
          <div className="hand second" style={hand(secDeg)} />
          <div className="pin" />
        </div>
      </div>

      <div className="clock-digital">{pad(h)}:{pad(m)}:{pad(sec)}</div>
    </div>
  )
}
