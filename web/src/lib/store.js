// The dashboard's persistent state lives in one config object that the Go
// backend stores verbatim. This module defines its shape, the default starter
// dashboard, and a debounced save so dragging widgets doesn't spam the server.

import { api } from './api'
import { WIDGETS } from '../widgets'

// A unique id for a new widget. crypto.randomUUID is available in modern
// browsers / the webview runtime.
export const newId = () =>
  (crypto?.randomUUID?.() || 'w' + Math.random().toString(36).slice(2))

// Default settings for each widget type when freshly added.
export const widgetDefaults = {
  clock: { title: 'Clock', settings: { face: 'flip', showSeconds: true, timezone: '' } },
  weather: { title: 'Weather', settings: { auto: true, lat: null, lon: null, place: '', tempUnit: 'celsius', windUnit: 'kmh' } },
  rss: {
    title: 'RSS',
    settings: {
      // Sensible starter feeds; add/remove via the widget's ⚙ button. Each feed
      // is { url, name }; an empty name falls back to the site's own title.
      feeds: [
        { url: 'https://techcrunch.com/feed/', name: '' },
        { url: 'https://www.socialmediatoday.com/feeds/news/', name: '' },
      ],
      // Filter groups: each is { title, words[] }; an item joins a group if its
      // title contains any of the words. Empty = feeds show as one list.
      filters: [],
      length: 50,
    },
    state: { read: [], ignored: [], seen: [] },
  },
  hackernews: { title: 'Hacker News', settings: { limit: 30 }, state: { read: [] } },
  markets: { title: 'Markets', settings: { symbols: ['AAPL', 'BTC-USD', '^GSPC'] } },
  calendar: { title: 'Calendar', settings: {} },
  scholarone: {
    title: 'ScholarOne',
    settings: {
      // The journal sites to query. Each is { key, name, url, enabled }. All
      // three start enabled; edit/rename/add via the widget's ⚙ button.
      sites: [
        { key: 'isr', name: 'Information Systems Research', url: 'https://mc.manuscriptcentral.com/isr', enabled: true },
        { key: 'ms', name: 'Management Science', url: 'https://mc.manuscriptcentral.com/ms', enabled: true },
        { key: 'misq', name: 'MIS Quarterly', url: 'https://mc.manuscriptcentral.com/misq', enabled: true },
      ],
      // When true, one username/password is used for every site (the common case).
      sameCreds: true,
    },
    // Cached results from the last retrieval (no credentials). Persists until the
    // next retrieve so the dashboard shows the latest status after a reload.
    state: { results: [], retrievedAt: '' },
  },
}

// Sensible default grid size (cols=12) for each new widget type.
export const widgetSize = {
  clock: { w: 3, h: 4 },
  weather: { w: 4, h: 6 },
  rss: { w: 4, h: 8 },
  hackernews: { w: 4, h: 8 },
  markets: { w: 4, h: 9 },
  calendar: { w: 4, h: 6 },
  scholarone: { w: 5, h: 9 },
}

// The starter dashboard shown on first run (no saved config yet).
export function defaultConfig() {
  const mk = (type) => ({ id: newId(), type, ...structuredClone(widgetDefaults[type]) })
  const widgets = [mk('clock'), mk('weather'), mk('hackernews'), mk('markets')]
  // Lay them out left-to-right on the 12-col grid.
  const layout = [
    { i: widgets[0].id, x: 0, y: 0, ...widgetSize.clock },
    { i: widgets[1].id, x: 3, y: 0, ...widgetSize.weather },
    { i: widgets[2].id, x: 7, y: 0, ...widgetSize.hackernews },
    { i: widgets[3].id, x: 0, y: 4, ...widgetSize.markets },
  ]
  return {
    version: 1,
    settings: {
      brandName: 'PerchBoard',// name shown top-left; editable in Settings
      backgroundMode: 'none', // 'none' | 'static' | 'shuffle'
      backgrounds: [],        // filenames returned by upload
      shuffleSeconds: 120,
      fontFamily: 'Inter',    // key into FONTS (see lib/fonts.js)
      fontScale: 1,           // 0.9 (S) | 1 (M) | 1.15 (L) | 1.3 (XL)
    },
    widgets,
    layout,
  }
}

// Load config from the server, falling back to defaults if empty/unsaved.
// Also drops any widget whose type no longer exists in the registry (e.g. a
// removed widget left over in a saved config) so the UI never shows a broken
// "Unknown widget" tile.
export async function loadConfig() {
  try {
    const cfg = await api.loadConfig()
    if (!cfg || !cfg.widgets) return defaultConfig()
    const widgets = cfg.widgets.filter((w) => WIDGETS[w.type])
    const keep = new Set(widgets.map((w) => w.id))
    const layout = (cfg.layout || []).filter((l) => keep.has(l.i))
    return { ...cfg, widgets, layout }
  } catch {
    return defaultConfig()
  }
}

// Debounced save: many quick mutations (drag, resize) collapse into one write.
let saveTimer = null
export function saveConfig(cfg, delay = 600) {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    api.saveConfig(cfg).catch((e) => console.warn('config save failed', e))
  }, delay)
}
