// Background-aware text contrast.
//
// When a background photo is shown, the UI text should auto-adjust to stay
// readable. We sample the image's average luminance on a
// tiny offscreen canvas and decide whether the foreground should be light or
// dark, then expose the result as CSS variables the whole app reads.

// averageLuminance loads an image URL and returns its mean perceived luminance
// in [0,1]. Downscaled to 16x16 for speed; this is plenty for an average.
export function averageLuminance(url) {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.crossOrigin = 'anonymous'
    img.onload = () => {
      const size = 16
      const canvas = document.createElement('canvas')
      canvas.width = size
      canvas.height = size
      const ctx = canvas.getContext('2d')
      ctx.drawImage(img, 0, 0, size, size)
      const { data } = ctx.getImageData(0, 0, size, size)
      let total = 0
      for (let i = 0; i < data.length; i += 4) {
        // Rec. 709 luma weights.
        const lum = 0.2126 * data[i] + 0.7152 * data[i + 1] + 0.0722 * data[i + 2]
        total += lum
      }
      resolve(total / (size * size) / 255)
    }
    img.onerror = reject
    img.src = url
  })
}

// applyContrast sets the document's CSS variables based on a luminance value.
// Dark background -> light text (and vice versa). When no background is active,
// callers pass `null` to restore the default dark theme.
export function applyContrast(luminance) {
  const root = document.documentElement
  if (luminance == null) {
    // Default theme: dark app, light text.
    root.style.setProperty('--fg', '#f5f5f7')
    root.style.setProperty('--fg-dim', 'rgba(245,245,247,0.65)')
    root.style.setProperty('--panel', 'rgba(20,20,24,0.55)')
    root.style.setProperty('--panel-solid', '#15151a')
    root.style.setProperty('--shadow', '0 1px 2px rgba(0,0,0,0.6)')
    return
  }
  const dark = luminance < 0.5 // background is dark -> use light text
  if (dark) {
    root.style.setProperty('--fg', '#ffffff')
    root.style.setProperty('--fg-dim', 'rgba(255,255,255,0.75)')
    root.style.setProperty('--panel', 'rgba(0,0,0,0.35)')
    root.style.setProperty('--panel-solid', 'rgba(20,20,24,0.85)')
    root.style.setProperty('--shadow', '0 1px 3px rgba(0,0,0,0.8)')
  } else {
    root.style.setProperty('--fg', '#0a0a0a')
    root.style.setProperty('--fg-dim', 'rgba(0,0,0,0.65)')
    root.style.setProperty('--panel', 'rgba(255,255,255,0.45)')
    root.style.setProperty('--panel-solid', 'rgba(255,255,255,0.85)')
    root.style.setProperty('--shadow', '0 1px 3px rgba(255,255,255,0.6)')
  }
}
