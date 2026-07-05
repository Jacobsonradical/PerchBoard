// Selectable fonts (loaded in index.html, with system fallbacks). The key is
// stored in settings.fontFamily; applyFont() pushes the stack into a CSS var.

export const FONTS = {
  Inter:   { label: 'Inter',   stack: "'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif" },
  Nunito:  { label: 'Nunito',  stack: "'Nunito', 'Inter', sans-serif" },
  System:  { label: 'System',  stack: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif" },
  Lora:    { label: 'Lora',    stack: "'Lora', Georgia, 'Times New Roman', serif" },
  Mono:    { label: 'Mono',    stack: "'JetBrains Mono', ui-monospace, SFMono-Regular, Menlo, monospace" },
}

export const FONT_SCALES = [
  { label: 'S', value: 0.9 },
  { label: 'M', value: 1 },
  { label: 'L', value: 1.15 },
  { label: 'XL', value: 1.3 },
]

// applyFont sets the CSS variables the stylesheet reads. Called whenever the
// font settings change.
export function applyFont(fontFamily, fontScale) {
  const root = document.documentElement
  const font = FONTS[fontFamily] || FONTS.Inter
  root.style.setProperty('--font', font.stack)
  root.style.setProperty('--font-scale', String(fontScale || 1))
}
