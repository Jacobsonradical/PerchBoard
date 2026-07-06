import React, { useState, useEffect, useRef } from 'react'
import { usePoll } from '../lib/usePoll'
import { api } from '../lib/api'
import { toast } from '../lib/notify'

// RSSWidget features:
//  - one tab per site (feed), with an editable tab/site name
//  - filter "groups": each group has a title + a set of words; an item matches
//    the group if its title contains ANY of the words. The group's title is the
//    section header (so several words can live under one custom title).
//  - configurable length; the body scrolls within the fixed widget height
//  - per-item "Ignore" removes it permanently (stored in state.ignored) after
//    a 3-min undo grace period that survives reloads (state.pendingRemovals)
//  - per-item "Save" moves it into the Saved tab (a "transform" of a live item
//    into a saved one — same site + filter sections, just persisted)
//  - notifications (toast + native) for newly-arrived items
//  - read items dim and stop "shining" (stored in state.read)
//  - Saved items render flat (not highlighted) and are themselves tabbed by site
//    and grouped into the same filter sections.
//
// Feeds/filters are added/removed via buttons in the settings panel.

const timeAgo = (iso) => {
  if (!iso) return ''
  const diff = (Date.now() - new Date(iso).getTime()) / 1000
  if (diff < 3600) return `${Math.max(1, Math.round(diff / 60))}m`
  if (diff < 86400) return `${Math.round(diff / 3600)}h`
  return `${Math.round(diff / 86400)}d`
}

const shortUrl = (u) => { try { return new URL(u).hostname.replace(/^www\./, '') } catch { return u } }
const splitWords = (v) => v.split(',').map((w) => w.trim()).filter(Boolean)

// --- Backward-compatible normalizers -------------------------------------
// Older configs stored feeds as plain URL strings and filters as plain words.
// Normalize both to the richer shape the UI now uses so old saved configs keep
// working; the next save rewrites them in the new form.
const normFeed = (f) =>
  typeof f === 'string' ? { url: f, name: '' } : { url: f.url, name: f.name || '' }
const normFilter = (f) =>
  typeof f === 'string' ? { title: f, words: [f] } : { title: f.title || '', words: f.words || [] }

export default function RSSWidget({ widget, onChange }) {
  const s = widget.settings
  const st = widget.state || { read: [], ignored: [], seen: [] }
  const feeds = (s.feeds || []).map(normFeed)
  const filters = (s.filters || []).map(normFilter)

  // Smart (LLM) filtering (3e): the display names of the filter groups, and a
  // signature so the cached classifications can be dropped when the groups change.
  const smartFilter = !!s.smartFilter
  const groupNames = filters.map((f) => f.title || (f.words[0] || 'Filter'))
  // The leading version tag lets a classifier change (e.g. the id-alignment fix)
  // invalidate any cached results computed by the old logic.
  const llmSig = 'v2|' + groupNames.join('|')
  const classifyingRef = useRef(false)
  const probedRef = useRef(false) // whether we've checked the LLM connection this session
  const [llmError, setLlmError] = useState('') // last smart-filter failure, if any

  const [showSettings, setShowSettings] = useState(feeds.length === 0)
  const [collapsed, setCollapsed] = useState(() => new Set()) // collapsed section names
  // Active top tab: a feed url, or 'saved'. Default to the first site.
  const [tab, setTab] = useState(() => feeds[0]?.url || 'saved')
  // Active site sub-tab within Saved: 'all' or a saved site key.
  const [savedSite, setSavedSite] = useState('all')
  // Items in their "grace period" after the user clicked remove/ignore (3c).
  // They stay in the list, shown as an undoable "removing…" row, and are only
  // really dropped when the grace period expires. The pending entries live in
  // widget.state (guid -> {kind, at}) — NOT in component memory — so a page
  // refresh or closing the dashboard keeps the removal in effect: on reload the
  // row comes back struck-through with Undo, and finalizes 3 min after the
  // original click (immediately, if that moment already passed).
  const pendingMap = st.pendingRemovals || {} // guid -> { kind: 'ignore'|'unsave', at: ms }
  const removeTimers = useRef(new Map()) // guid -> timeout id
  // Always-fresh refs so a timer that fires minutes later writes against the
  // latest widget/onChange (never a stale snapshot that could clobber other edits).
  const widgetRef = useRef(widget); widgetRef.current = widget
  const onChangeRef = useRef(onChange); onChangeRef.current = onChange
  const RESERVE_MS = 3 * 60 * 1000 // keep a removed item recoverable for 3 min

  // Clear any outstanding grace timers if the widget unmounts. The pending
  // entries stay in widget.state, so they are picked up again on the next mount.
  useEffect(() => () => {
    for (const t of removeTimers.current.values()) clearTimeout(t)
  }, [])

  const toggleCat = (name) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      next.has(name) ? next.delete(name) : next.add(name)
      return next
    })

  // Fetch every configured feed in parallel; tag each item with its feed url so
  // we can group items back into per-site tabs (source titles can collide/be
  // empty, the url is the stable key).
  const { data, error, loading } = usePoll(
    async () => {
      const lists = await Promise.all(
        feeds.map(async (f) => {
          const items = await api.rss(f.url).catch(() => []) // one bad feed won't kill the rest
          return items.map((it) => ({ ...it, feedUrl: f.url }))
        }),
      )
      const merged = lists.flat()
      merged.sort((a, b) => (b.published || '').localeCompare(a.published || ''))
      return merged
    },
    5 * 60 * 1000, // refresh every 5 min
    [feeds.map((f) => f.url).join('|')],
  )

  const patch = (p) => onChange({ ...widget, ...p })
  const setSettings = (p) => patch({ settings: { ...s, ...p } })
  const markRead = (guid) => {
    if (st.read.includes(guid)) return
    patch({ state: { ...st, read: [...st.read, guid].slice(-1000) } })
  }
  // Saved items keep the full payload (incl. feedUrl + source) so the Saved tab
  // can re-group them by site and filter exactly like the live feed.
  const saved = st.saved || []
  const save = (item) => {
    if (saved.some((x) => x.guid === item.guid)) return
    patch({ state: { ...st, saved: [
      { guid: item.guid, title: item.title, link: item.link, source: item.source, feedUrl: item.feedUrl, published: item.published },
      ...saved,
    ] } })
  }
  // Start a grace period instead of removing outright (3c) — applies to BOTH a
  // live item's "Ignore" and a saved item's "Remove". The entry is written to
  // widget.state immediately (so it survives refresh) and shown as an undoable
  // "removing…" strip; the timer effect below finalizes it unless undone.
  //   kind 'ignore' -> live feed item (will be added to state.ignored)
  //   kind 'unsave' -> saved item    (will be dropped from state.saved)
  const scheduleRemove = (guid, kind) => {
    patch({ state: { ...st, pendingRemovals: { ...pendingMap, [guid]: { kind, at: Date.now() } } } })
  }
  // Commit the removal once the grace period expires. Reads the latest
  // widget/onChange via refs so a timer firing minutes later can't overwrite
  // unrelated changes made in the meantime. The pending entry is cleared in the
  // same write as the removal itself, so the two can never get out of sync.
  const finalizeRemove = (guid) => {
    const timers = removeTimers.current
    if (timers.has(guid)) { clearTimeout(timers.get(guid)); timers.delete(guid) }
    const w = widgetRef.current
    const wst = w.state || {}
    const pend = { ...(wst.pendingRemovals || {}) }
    const entry = pend[guid]
    if (!entry) return // already undone (or finalized by another write)
    delete pend[guid]
    if (entry.kind === 'unsave') {
      onChangeRef.current({ ...w, state: { ...wst, pendingRemovals: pend, saved: (wst.saved || []).filter((x) => x.guid !== guid) } })
    } else { // 'ignore'
      const ignored = (wst.ignored || []).includes(guid)
        ? wst.ignored
        : [...(wst.ignored || []), guid].slice(-1000)
      onChangeRef.current({ ...w, state: { ...wst, pendingRemovals: pend, ignored } })
    }
  }
  // Cancel a pending removal and keep the item.
  const undoRemove = (guid) => {
    const pend = { ...pendingMap }
    delete pend[guid]
    patch({ state: { ...st, pendingRemovals: pend } })
  }
  // Keep a finalize timer alive for every pending entry. Because the entries
  // are persisted with their click time, this also covers reload: an entry
  // whose 3 minutes already elapsed while the page was closed gets a ~0ms
  // timer and is finalized right away; a younger one waits out its remainder.
  useEffect(() => {
    const timers = removeTimers.current
    for (const [guid, p] of Object.entries(pendingMap)) {
      if (!timers.has(guid)) {
        const left = Math.max(0, (p.at || 0) + RESERVE_MS - Date.now())
        timers.set(guid, setTimeout(() => finalizeRemove(guid), left))
      }
    }
    // Drop timers whose entry is gone (undone) so a stale timer can't re-remove.
    for (const guid of [...timers.keys()]) {
      if (!(guid in pendingMap)) { clearTimeout(timers.get(guid)); timers.delete(guid) }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(pendingMap)])

  // Notify on newly-seen items (5.4). Skip the very first population.
  const firstLoad = useRef(true)
  useEffect(() => {
    if (!data) return
    const seen = new Set(st.seen)
    const fresh = data.filter((it) => it.guid && !seen.has(it.guid))
    if (firstLoad.current) {
      firstLoad.current = false
    } else if (fresh.length > 0) {
      toast({ title: `📰 ${fresh.length} new in ${widget.title}`, body: fresh[0].title })
    }
    if (fresh.length > 0) {
      const nextSeen = [...data.map((it) => it.guid), ...st.seen].filter(Boolean)
      patch({ state: { ...st, seen: Array.from(new Set(nextSeen)).slice(0, 500) } })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data])

  // Smart filtering (3e): when enabled, ask the backend LLM to sort not-yet-seen
  // live items into the filter groups, caching the result per guid. Writes go
  // through the refs so a slow classification can't clobber concurrent edits.
  useEffect(() => {
    if (!smartFilter || filters.length === 0 || !data) return
    // Group titles changed → the cache is stale; drop it and re-classify next tick.
    if ((st.llmSig || '') !== llmSig) {
      const w = widgetRef.current, wst = w.state || {}
      onChangeRef.current({ ...w, state: { ...wst, llmCats: {}, llmSig } })
      return
    }
    if (classifyingRef.current) return
    const cached = st.llmCats || {}
    const ignoredSet = new Set(st.ignored || [])
    const savedGuids = new Set((st.saved || []).map((x) => x.guid))
    const pending = data
      .filter((it) => it.guid && !ignoredSet.has(it.guid) && !savedGuids.has(it.guid) && !(it.guid in cached))
      .slice(0, 60) // one batch per refresh keeps calls (and cost) bounded
    if (pending.length === 0) {
      // Everything is already classified, so no real call would be made. Do one
      // cheap connectivity probe per session so a broken key still trips the
      // failure banner even when there are no new items to classify.
      if (!probedRef.current) {
        probedRef.current = true
        api.llmClassify([{ id: '__probe__', title: 'connectivity check' }], groupNames)
          .then(() => setLlmError(''))
          .catch((e) => setLlmError(e.message || 'classification failed'))
      }
      return
    }
    probedRef.current = true // a real classification also counts as a connection check
    classifyingRef.current = true
    api.llmClassify(pending.map((it) => ({ id: it.guid, title: it.title })), groupNames)
      .then((res) => {
        const w = widgetRef.current, wst = w.state || {}
        const merged = { ...(wst.llmCats || {}) }
        for (const it of pending) merged[it.guid] = res[it.guid] || []
        onChangeRef.current({ ...w, state: { ...wst, llmCats: merged, llmSig } })
        setLlmError('') // recovered
      })
      .catch((e) => setLlmError(e.message || 'classification failed'))
      .finally(() => { classifyingRef.current = false })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [smartFilter, data, llmSig])

  if (showSettings) {
    return <RSSSettings widget={widget} feeds={feeds} filters={filters} setSettings={setSettings} done={() => setShowSettings(false)} />
  }
  if (loading && !data) return <div className="center-note">Loading feeds…</div>
  if (feeds.length === 0) return <div className="center-note">No feeds yet. Click ⚙ to add one.</div>

  // Read / ignored / saved sets used for filtering + badges.
  const ignored = new Set(st.ignored)
  const read = new Set(st.read)
  const savedSet = new Set(saved.map((x) => x.guid))
  const inFeed = (it) => !ignored.has(it.guid) && !savedSet.has(it.guid)

  // Resolve the active top tab (fall back if its feed was removed).
  let current = tab
  if (current !== 'saved' && !feeds.some((f) => f.url === current)) current = feeds[0]?.url || 'saved'

  // Display name for a site tab: explicit name, else the feed's own title from
  // the fetched items, else the hostname.
  const feedName = (f) => {
    if (f.name) return f.name
    const item = (data || []).find((it) => it.feedUrl === f.url && it.source)
    return item?.source || shortUrl(f.url)
  }

  // --- Saved-tab grouping helpers ---
  // Map each current feed's own source title (from live items) back to its url,
  // so legacy saved items — which only carry a `source`, not a `feedUrl` — can be
  // folded into the matching feed's tab instead of forming a stale duplicate (3b).
  const sourceToFeedUrl = {}
  for (const f of feeds) {
    const item = (data || []).find((it) => it.feedUrl === f.url && it.source)
    if (item?.source) sourceToFeedUrl[item.source] = f.url
  }
  // Group a saved item by its feed. Prefer the stable feed url; fall back to
  // folding a legacy (url-less) item into a current feed via its source, so a
  // renamed tab stays in sync and never splits old/new saves apart.
  const savedKey = (x) => {
    if (x.feedUrl && feeds.some((f) => f.url === x.feedUrl)) return x.feedUrl
    if (x.source && sourceToFeedUrl[x.source]) return sourceToFeedUrl[x.source]
    return x.feedUrl || x.source || 'unknown'
  }
  const savedSiteKeys = []
  const seenKeys = new Set()
  for (const x of saved) {
    const k = savedKey(x)
    if (!seenKeys.has(k)) { seenKeys.add(k); savedSiteKeys.push(k) }
  }
  const savedSiteName = (k) => {
    const f = feeds.find((ff) => ff.url === k)
    if (f) return feedName(f)
    const item = saved.find((x) => savedKey(x) === k)
    return item?.source || shortUrl(k)
  }
  let curSavedSite = savedSite
  if (curSavedSite !== 'all' && !savedSiteKeys.includes(curSavedSite)) curSavedSite = 'all'

  // Shared "removing…" row shown while an item (live or saved) is in its grace
  // period (3c) — a struck-through title with a single Undo action.
  const pendingRow = (it) => (
    <li key={it.guid} className="feed-item pending-remove">
      <div style={{ flex: 1 }}>
        <span className="removing-title">{it.title}</span>
      </div>
      <div className="feed-actions">
        <button className="act undo" data-tip="Undo" aria-label="Undo remove" onClick={() => undoRemove(it.guid)}>↩ Undo</button>
      </div>
    </li>
  )

  // Live item row: shines when unread, dims when read; Save + Ignore actions.
  // Ignore now goes through the same grace period as saved-remove (3c).
  // Tooltips use data-tip (custom CSS) instead of the native title, which the
  // browser often skips showing after a click or when the row shifts (3d).
  const liveRow = (it) => {
    if (pendingMap[it.guid]) return pendingRow(it)
    return (
      <li key={it.guid} className={'feed-item' + (read.has(it.guid) ? ' read' : '')}>
        <div style={{ flex: 1 }}>
          <a href={it.link} target="_blank" rel="noreferrer" onClick={() => markRead(it.guid)}>{it.title}</a>
          <div className="meta">{it.source} · {timeAgo(it.published)}</div>
        </div>
        <div className="feed-actions">
          <button className="act" data-tip="Save for later" aria-label="Save for later" onClick={() => save(it)}>🔖</button>
          <button className="act" data-tip="Ignore" aria-label="Ignore" onClick={() => scheduleRemove(it.guid, 'ignore')}>✕</button>
        </div>
      </li>
    )
  }

  // Saved item row: flat (never highlighted), with a grace-period remove (3c).
  const savedRow = (it) => {
    if (pendingMap[it.guid]) return pendingRow(it)
    return (
      <li key={it.guid} className="feed-item flat">
        <div style={{ flex: 1 }}>
          <a href={it.link} target="_blank" rel="noreferrer" onClick={() => markRead(it.guid)}>{it.title}</a>
          <div className="meta">{it.source} · {timeAgo(it.published)}</div>
        </div>
        <div className="feed-actions">
          <button className="act" data-tip="Remove from saved" aria-label="Remove from saved" onClick={() => scheduleRemove(it.guid, 'unsave')}>✕</button>
        </div>
      </li>
    )
  }

  return (
    <div>
      <div className="rss-tabbar">
        <div className="rss-tabs">
          {feeds.map((f) => {
            const unread = (data || []).filter((it) => it.feedUrl === f.url && inFeed(it) && !read.has(it.guid)).length
            return (
              <button
                key={f.url}
                className={'rss-tab' + (current === f.url ? ' active' : '')}
                onClick={() => setTab(f.url)}
                title={f.url}
              >
                {feedName(f)}{unread > 0 && <span className="tab-badge">{unread}</span>}
              </button>
            )
          })}
          <button className={'rss-tab' + (current === 'saved' ? ' active' : '')} onClick={() => setTab('saved')}>
            🔖 Saved{saved.length > 0 && <span className="tab-badge">{saved.length}</span>}
          </button>
        </div>
        <button className="head-btn" onClick={() => setShowSettings(true)} title="Feed settings">⚙</button>
      </div>

      {current === 'saved' ? (
        saved.length === 0 ? (
          <div className="center-note">No saved items yet. Click 🔖 on a feed item to keep it here.</div>
        ) : (
          <>
            {/* Site sub-tabs within Saved */}
            <div className="rss-subtabs">
              <button className={'rss-subtab' + (curSavedSite === 'all' ? ' active' : '')} onClick={() => setSavedSite('all')}>All</button>
              {savedSiteKeys.map((k) => (
                <button key={k} className={'rss-subtab' + (curSavedSite === k ? ' active' : '')} onClick={() => setSavedSite(k)}>
                  {savedSiteName(k)}
                </button>
              ))}
            </div>
            <CategoryList
              categories={categorize(
                (curSavedSite === 'all' ? saved : saved.filter((x) => savedKey(x) === curSavedSite))
                  .slice()
                  .sort((a, b) => (b.published || '').localeCompare(a.published || '')),
                filters,
              )}
              collapsed={collapsed}
              toggleCat={toggleCat}
              renderRow={savedRow}
            />
          </>
        )
      ) : (
        <>
          {error && <div className="err-note">Some feeds failed: {error}</div>}
          {smartFilter && llmError && (
            <div className="err-note">⚠ Smart filter unavailable — word matching is handling filtering. ({llmError})</div>
          )}
          <CategoryList
            categories={categorize(
              (data || []).filter((it) => it.feedUrl === current && inFeed(it)).slice(0, s.length || 50),
              filters,
              { enabled: smartFilter, cats: st.llmCats || {} },
            )}
            collapsed={collapsed}
            toggleCat={toggleCat}
            renderRow={liveRow}
          />
        </>
      )}
    </div>
  )
}

// CategoryList renders filter sections with collapsible headers. With a single
// "All" category (no filters) it renders a bare list (no header).
function CategoryList({ categories, collapsed, toggleCat, renderRow }) {
  if (categories.length === 0) return <div className="center-note">Nothing here.</div>
  return categories.map(({ name, items }) => {
    const hasHeader = categories.length > 1
    const isCollapsed = hasHeader && collapsed.has(name)
    return (
      <div key={name}>
        {hasHeader && (
          <div className="feed-cat clickable" onClick={() => toggleCat(name)}>
            <span className="cat-arrow">{isCollapsed ? '▶' : '▼'}</span>
            {name}
            <span className="cat-count">{items.length}</span>
          </div>
        )}
        {!isCollapsed && <ul className="feed-list">{items.map(renderRow)}</ul>}
      </div>
    )
  })
}

// categorize buckets items into filter groups. Word matching (title contains ANY
// of the group's words) always runs — it's the reliable default. When `smart` is
// enabled, the LLM's classification for an item is added on top (a UNION): an item
// joins a group if the words match OR the LLM put it there, so the two reinforce
// each other and an LLM hiccup never drops items the words would have caught.
// Items matching nothing fall into "Other". With no filters, one "All" bucket.
function categorize(items, filters, smart) {
  if (!filters || filters.length === 0) return [{ name: 'All', items }]
  const buckets = filters.map((f) => ({
    name: f.title || (f.words[0] || 'Filter'),
    items: [],
    tests: (f.words || []).map((w) => w.toLowerCase()).filter(Boolean),
  }))
  const byName = new Map(buckets.map((b) => [b.name, b]))
  const other = { name: 'Other', items: [] }
  const smartOn = smart && smart.enabled
  for (const it of items) {
    const title = (it.title || '').toLowerCase()
    const hits = new Set()
    // Word match (always).
    for (const b of buckets) {
      if (b.tests.some((t) => title.includes(t))) hits.add(b.name)
    }
    // LLM match (union) for items that have been classified.
    if (smartOn) {
      const llmCats = (smart.cats || {})[it.guid]
      if (llmCats) for (const name of llmCats) if (byName.has(name)) hits.add(name)
    }
    if (hits.size === 0) other.items.push(it)
    else for (const name of hits) byName.get(name).items.push(it)
  }
  return [...buckets, other].filter((b) => b.items.length > 0).map(({ name, items }) => ({ name, items }))
}

// RSSSettings: add/remove feeds (with editable names) + filter groups, set length.
function RSSSettings({ widget, feeds, filters, setSettings, done }) {
  const s = widget.settings
  const [feed, setFeed] = useState('')
  const [ftitle, setFtitle] = useState('')
  const [fwords, setFwords] = useState('')

  const writeFeeds = (next) => setSettings({ feeds: next })
  const writeFilters = (next) => setSettings({ filters: next })

  const addFeed = () => {
    const u = feed.trim()
    if (u && !feeds.some((f) => f.url === u)) writeFeeds([...feeds, { url: u, name: '' }])
    setFeed('')
  }
  const renameFeed = (i, name) => writeFeeds(feeds.map((f, j) => (j === i ? { ...f, name } : f)))
  const removeFeed = (i) => writeFeeds(feeds.filter((_, j) => j !== i))

  const addFilter = () => {
    const title = ftitle.trim()
    const words = splitWords(fwords)
    const finalWords = words.length ? words : (title ? [title] : [])
    if (title && finalWords.length) writeFilters([...filters, { title, words: finalWords }])
    setFtitle(''); setFwords('')
  }
  const updateFilter = (i, patch) => writeFilters(filters.map((f, j) => (j === i ? { ...f, ...patch } : f)))
  const removeFilter = (i) => writeFilters(filters.filter((_, j) => j !== i))

  return (
    <div>
      <div className="section">
        <label>Feeds & tab names</label>
        <div className="feed-edit-list">
          {feeds.map((f, i) => (
            <div className="feed-edit-row" key={f.url}>
              <input
                className="feed-name-input"
                value={f.name}
                placeholder={shortUrl(f.url)}
                onChange={(e) => renameFeed(i, e.target.value)}
              />
              <span className="feed-url" title={f.url}>{shortUrl(f.url)}</span>
              <button className="chip-x" title="Remove feed" onClick={() => removeFeed(i)}>✕</button>
            </div>
          ))}
          {feeds.length === 0 && <span className="muted-note">none yet</span>}
        </div>
        <div className="inline-add">
          <input value={feed} onChange={(e) => setFeed(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && addFeed()} placeholder="https://example.com/feed.xml" />
          <button className="btn primary" onClick={addFeed}>Add</button>
        </div>
      </div>

      <div className="section">
        <label>Filter groups (a title + words; matches any word)</label>
        <div className="feed-edit-list">
          {filters.map((f, i) => (
            <div className="filter-edit-row" key={i}>
              <input
                className="feed-name-input"
                value={f.title}
                placeholder="Title"
                onChange={(e) => updateFilter(i, { title: e.target.value })}
              />
              <input
                className="filter-words-input"
                value={f.words.join(', ')}
                placeholder="words: AI, Elon Musk"
                onChange={(e) => updateFilter(i, { words: splitWords(e.target.value) })}
              />
              <button className="chip-x" title="Remove group" onClick={() => removeFilter(i)}>✕</button>
            </div>
          ))}
          {filters.length === 0 && <span className="muted-note">none — feeds show as one list</span>}
        </div>
        <div className="inline-add">
          <input value={ftitle} onChange={(e) => setFtitle(e.target.value)} placeholder="Title (e.g. AI)" style={{ flex: '0 0 35%' }} />
          <input value={fwords} onChange={(e) => setFwords(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && addFilter()} placeholder="words: AI, Elon Musk" />
          <button className="btn" onClick={addFilter}>Add</button>
        </div>
      </div>

      <div className="section">
        <label>Max items per site: {s.length}</label>
        <input type="range" min="10" max="100" step="5" value={s.length} onChange={(e) => setSettings({ length: Number(e.target.value) })} style={{ width: '100%' }} />
      </div>

      <div className="section">
        <label className="s1-check">
          <input
            type="checkbox"
            checked={!!s.smartFilter}
            onChange={(e) => setSettings({ smartFilter: e.target.checked })}
          />
          Smart filtering (LLM) — sort items into groups by meaning
        </label>
        <div className="muted-note" style={{ marginTop: 4 }}>
          Uses your filter-group titles instead of the words. Requires an API key in
          the dashboard ⚙ Settings; falls back to word matching when off or unset.
        </div>
      </div>

      <button className="btn primary" onClick={done}>Done</button>
    </div>
  )
}
