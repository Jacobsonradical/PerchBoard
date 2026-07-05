import React, { useEffect, useState } from 'react'
import { averageLuminance, applyContrast } from '../lib/contrast'

// Background renders the photo layer behind the dashboard and keeps text
// contrast readable:
//   - mode 'none'    -> plain dark theme
//   - mode 'static'  -> first background image
//   - mode 'shuffle' -> cycles through all images every shuffleSeconds
// Whenever the visible image changes we sample its luminance and flip the UI
// foreground colours light/dark accordingly.
export default function Background({ settings }) {
  const { backgroundMode, backgrounds = [], shuffleSeconds = 120 } = settings
  const [index, setIndex] = useState(0)

  const active = backgroundMode !== 'none' && backgrounds.length > 0
  const list = backgroundMode === 'shuffle' ? backgrounds : backgrounds.slice(0, 1)
  const current = active ? list[index % list.length] : null
  const url = current ? `/backgrounds/${current}` : null

  // Shuffle timer.
  useEffect(() => {
    if (backgroundMode !== 'shuffle' || backgrounds.length < 2) return
    const id = setInterval(() => setIndex((i) => i + 1), Math.max(10, shuffleSeconds) * 1000)
    return () => clearInterval(id)
  }, [backgroundMode, backgrounds.length, shuffleSeconds])

  // Reset index if the list shrinks/changes.
  useEffect(() => { setIndex(0) }, [backgroundMode, backgrounds.join('|')])

  // Recompute contrast whenever the visible image changes.
  useEffect(() => {
    if (!url) { applyContrast(null); return }
    let cancelled = false
    averageLuminance(url)
      .then((lum) => { if (!cancelled) applyContrast(lum) })
      .catch(() => applyContrast(null))
    return () => { cancelled = true }
  }, [url])

  if (!url) return null
  // key={url} remounts the layer on each change so the fade-in / Ken Burns
  // animation replays — giving the shuffle a smooth transition.
  return (
    <>
      <div key={url} className="bg-layer bg-animate" style={{ backgroundImage: `url("${url}")` }} />
      <div className="bg-scrim" />
    </>
  )
}
