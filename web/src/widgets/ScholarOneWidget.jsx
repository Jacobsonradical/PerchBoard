import React, { useState, useEffect } from 'react'
import { api } from '../lib/api'
import { hasVault, clearVault, saveVault, openVault } from '../lib/credvault'

// ScholarOneWidget: on-demand retrieval of paper (Author) and review (Reviewer)
// status across ScholarOne journal sites. The user enters their login(s); we ask
// the local backend to drive a headless browser, log in to each site, and read
// the dashboards. Credentials live only in this component's memory for the
// duration of a retrieval — never in the dashboard config.
//
// Two modes for results caching:
//  - Default (no saved login): scraped results are cached in widget.state so they
//    survive a reload until the next retrieval.
//  - Saved login (task 4): the user has an encrypted, passphrase-locked login. Then
//    results are kept in MEMORY only for the session — nothing sensitive is written
//    to disk, and after a reload the widget shows only the unlock screen until the
//    master passphrase is entered.
export default function ScholarOneWidget({ widget, onChange }) {
  const s = widget.settings || {}
  const st = widget.state || { results: [], retrievedAt: '' }
  const sites = s.sites || []
  const enabledSites = sites.filter((x) => x.enabled !== false)
  const sameCreds = s.sameCreds !== false

  const [showSettings, setShowSettings] = useState(false)
  const [editingCreds, setEditingCreds] = useState(false) // force the form back even with cached results
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  // Credentials: kept in component state only, cleared after each retrieval
  // unless the user has opted to save them (task 4).
  const [shared, setShared] = useState({ username: '', password: '' })
  const [perSite, setPerSite] = useState({}) // key -> { username, password }

  // Keep the browser's own password manager from capturing / auto-filling these
  // fields — otherwise it would silently refill the login on reload, bypassing the
  // master passphrase and stashing the password in its own (possibly synced) store.
  // The reliable trick: fields start readOnly (browsers won't autofill a readOnly
  // field) and become editable only once the user focuses them; combined with
  // autoComplete="new-password" so a saved login isn't offered either.
  const [fieldsEditable, setFieldsEditable] = useState(false)
  const noAutofill = {
    autoComplete: 'new-password',
    readOnly: !fieldsEditable,
    onFocus: () => setFieldsEditable(true),
  }

  // --- saved-login (encrypted vault) state (task 4) ------------------------
  // The vault lives in the browser (localStorage), encrypted with a master
  // passphrase that is only ever held here in memory for the session. These are
  // seeded synchronously from the stored vault so the widget locks on the very
  // first render (no flash of cached results before the effect runs).
  const vaultExists = hasVault(widget.id)
  const [vaultPresent, setVaultPresent] = useState(vaultExists) // a saved login exists
  const [manual, setManual] = useState(false)             // one-off login, skip the vault
  const [saveOn, setSaveOn] = useState(vaultExists)       // user wants to save
  const [consent, setConsent] = useState(vaultExists)     // accepted the 4b warning (previously)
  const [passphrase, setPassphrase] = useState('')        // active master passphrase (memory only)
  const [pass2, setPass2] = useState('')                  // confirm when setting a new one
  const [unlockPass, setUnlockPass] = useState('')        // passphrase entry on the unlock screen
  const [vaultMsg, setVaultMsg] = useState('')            // unlock / forget feedback

  // Results for a protected (saved-login) widget live ONLY in memory for the
  // session — never in the persisted config — so nothing sensitive survives a
  // reload or sits in plaintext on disk. Non-protected widgets keep using the
  // persisted cache (widget.state) as before.
  const [sessionResults, setSessionResults] = useState(null)
  const [sessionRetrievedAt, setSessionRetrievedAt] = useState('')

  // If a saved login exists, make sure no plaintext results linger in the persisted
  // config from before this widget was protected.
  useEffect(() => {
    if (hasVault(widget.id) && ((st.results && st.results.length) || st.retrievedAt)) {
      update({ state: { results: [], retrievedAt: '' } })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Unlock the saved login: decrypt and retrieve immediately. The passphrase is
  // required for EVERY retrieval — we never keep the widget "unlocked" for the
  // session. The decrypted creds are passed straight to retrieve() (never stored
  // in state) and discarded when the retrieval ends.
  const doUnlock = async () => {
    setVaultMsg('')
    try {
      const v = await openVault(widget.id, unlockPass)
      setUnlockPass('')
      retrieve({ shared: v.shared || { username: '', password: '' }, perSite: v.perSite || {} })
    } catch (e) {
      setVaultMsg(e.message || 'Wrong passphrase.')
    }
  }

  // Forget the saved login entirely (delete the encrypted vault).
  const doForget = () => {
    clearVault(widget.id)
    setVaultPresent(false)
    setManual(false)
    setSaveOn(false)
    setConsent(false)
    setPassphrase('')
    setPass2('')
    setShared({ username: '', password: '' })
    setPerSite({})
    setVaultMsg('Saved login removed from this device.')
  }

  // Which view (papers/reviews) each result section shows; default papers.
  const [view, setView] = useState({})
  // Which journal tab is open; '' falls back to the first result.
  const [activeKey, setActiveKey] = useState('')

  // Protected = a saved login is (or is being) used; its results stay in memory.
  const protectedMode = saveOn || vaultPresent
  const results = protectedMode ? (sessionResults || []) : (st.results || [])
  const retrievedAt = protectedMode ? sessionRetrievedAt : (st.retrievedAt || '')
  const hasResults = results.length > 0
  const showForm = !hasResults || editingCreds

  // --- helpers --------------------------------------------------------------

  const update = (patch) => onChange({ ...widget, ...patch })
  const setSites = (next) => update({ settings: { ...s, sites: next } })

  const credFor = (key) => perSite[key] || { username: '', password: '' }
  const setCredFor = (key, patch) =>
    setPerSite((p) => ({ ...p, [key]: { ...credFor(key), ...patch } }))

  // `ov` optionally supplies { shared, perSite } directly — used when unlocking
  // auto-retrieves, since the decrypted creds aren't in state yet (setState is async).
  const retrieve = async (ov) => {
    setErr('')
    if (!enabledSites.length) {
      setErr('Add at least one journal site in settings (⚙).')
      return
    }
    const sh = ov?.shared || shared
    const ps = ov?.perSite || perSite
    const creds = enabledSites.map((site) => {
      const c = sameCreds ? sh : (ps[site.key] || { username: '', password: '' })
      return {
        key: site.key,
        name: site.name,
        url: site.url,
        username: (c.username || '').trim(),
        password: c.password || '',
      }
    })
    if (creds.some((c) => !c.username || !c.password)) {
      setErr('Enter a username and password for every site.')
      return
    }
    // If the user is enabling save for the first time, validate the master
    // passphrase before we do the slow retrieval (so we don't retrieve then fail).
    if (saveOn && !vaultPresent) {
      if (!consent) {
        setErr('Please accept the note to save your login, or uncheck “Save my login”.')
        return
      }
      if ((passphrase || '').length < 6) {
        setErr('Choose a master passphrase of at least 6 characters.')
        return
      }
      if (passphrase !== pass2) {
        setErr('The two master passphrases do not match.')
        return
      }
    }
    setBusy(true)
    try {
      const res = await api.scholarOne(creds)
      const now = new Date().toISOString()
      if (protectedMode) {
        // Keep protected results in memory only; never persist them to disk.
        setSessionResults(res)
        setSessionRetrievedAt(now)
        if ((st.results && st.results.length) || st.retrievedAt) {
          update({ state: { results: [], retrievedAt: '' } })
        }
      } else {
        update({ state: { results: res, retrievedAt: now } })
      }
      // Save the encrypted vault only when first creating it (a passphrase was just
      // set). An existing vault is never re-written on a normal retrieve.
      if (saveOn && !vaultPresent && passphrase) {
        try {
          await saveVault(widget.id, passphrase, { shared: sh, perSite: ps })
          setVaultPresent(true)
        } catch { /* saving is best-effort; a failure shouldn't lose the results */ }
      }
      // Always drop the credentials (and passphrase) from memory after a retrieval,
      // so the next one requires the passphrase again — nothing stays "unlocked".
      setEditingCreds(false)
      setManual(false)
      setPass2('')
      setPassphrase('')
      setShared({ username: '', password: '' })
      setPerSite({})
    } catch (e) {
      setErr(e.message || 'Retrieval failed.')
    } finally {
      setBusy(false)
    }
  }

  // --- settings -------------------------------------------------------------

  if (showSettings) {
    return (
      <div className="s1">
        <div className="section">
          <div className="s1-label">Journal sites</div>
          <div className="feed-edit-list">
            {sites.map((site, i) => (
              <div key={i} className="s1-site-edit">
                <input
                  type="checkbox"
                  title="Include in retrieval"
                  checked={site.enabled !== false}
                  onChange={(e) => {
                    const next = [...sites]
                    next[i] = { ...site, enabled: e.target.checked }
                    setSites(next)
                  }}
                />
                <div style={{ flex: 1 }}>
                  <input
                    className="feed-name-input"
                    value={site.name}
                    placeholder="Journal name"
                    onChange={(e) => {
                      const next = [...sites]
                      next[i] = { ...site, name: e.target.value }
                      setSites(next)
                    }}
                  />
                  <input
                    className="feed-name-input s1-url-input"
                    value={site.url}
                    placeholder="https://mc.manuscriptcentral.com/…"
                    onChange={(e) => {
                      const next = [...sites]
                      next[i] = { ...site, url: e.target.value }
                      setSites(next)
                    }}
                  />
                </div>
                <button
                  className="chip-x"
                  title="Remove site"
                  onClick={() => setSites(sites.filter((_, j) => j !== i))}
                >
                  ✕
                </button>
              </div>
            ))}
          </div>
          <button
            className="btn"
            style={{ marginTop: 8 }}
            onClick={() =>
              setSites([...sites, { key: 's' + Date.now(), name: '', url: '', enabled: true }])
            }
          >
            + Add site
          </button>
        </div>
        <button className="btn primary" onClick={() => setShowSettings(false)}>
          Done
        </button>
      </div>
    )
  }

  // --- retrieving -----------------------------------------------------------

  if (busy) {
    return (
      <div className="s1 s1-center">
        <div className="s1-spinner" />
        <div className="s1-retrieving">Retrieving…</div>
        <div className="s1-sub">
          Logging in to each journal site in the background. This can take up to a
          minute.
        </div>
      </div>
    )
  }

  // --- credential form ------------------------------------------------------

  if (showForm) {
    // A saved login exists: ALWAYS ask for the master passphrase before a retrieval
    // (the passphrase is never cached for the session). Entering it retrieves right
    // away. "Enter manually" allows a one-off login without touching the vault.
    if (vaultPresent && !manual) {
      return (
        <div className="s1">
          <div className="s1-head">
            <div className="s1-title">Unlock saved login</div>
            <button className="head-btn" onClick={() => setShowSettings(true)}>⚙</button>
          </div>
          <div className="s1-lock-note">
            🔒 Your login is saved and encrypted on this device. Enter your master
            passphrase to retrieve your status.
          </div>
          <input
            className="s1-input"
            type="password"
            placeholder="Master passphrase"
            {...noAutofill}
            value={unlockPass}
            onChange={(e) => setUnlockPass(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && doUnlock()}
          />
          {vaultMsg && <div className="err-note" style={{ marginTop: 8 }}>{vaultMsg}</div>}
          {err && <div className="err-note" style={{ marginTop: 8 }}>{err}</div>}
          <div className="s1-actions">
            <button className="btn primary" onClick={doUnlock}>Unlock &amp; retrieve</button>
            <button className="btn" onClick={() => { setManual(true); setSaveOn(false) }}>
              Enter manually
            </button>
          </div>
          {hasResults && (
            <button className="btn" style={{ marginTop: 8 }} onClick={() => setEditingCreds(false)}>
              Cancel
            </button>
          )}
          <button className="s1-forget" onClick={doForget}>Forget saved login</button>
        </div>
      )
    }
    return (
      <div className="s1">
        <div className="s1-head">
          <div className="s1-title">Retrieve paper &amp; review status</div>
          <button className="head-btn" onClick={() => setShowSettings(true)}>
            ⚙
          </button>
        </div>

        {!enabledSites.length && (
          <div className="center-note">No sites enabled. Add one in settings (⚙).</div>
        )}

        <label className="s1-check s1-samecreds">
          <input
            type="checkbox"
            checked={sameCreds}
            onChange={(e) => update({ settings: { ...s, sameCreds: e.target.checked } })}
          />
          Use the same username &amp; password for every site
        </label>

        {sameCreds ? (
          <div className="s1-cred-block">
            <input
              className="s1-input"
              placeholder="Username"
              {...noAutofill}
              value={shared.username}
              onChange={(e) => setShared({ ...shared, username: e.target.value })}
            />
            <input
              className="s1-input"
              type="password"
              placeholder="Password"
              {...noAutofill}
              value={shared.password}
              onChange={(e) => setShared({ ...shared, password: e.target.value })}
              onKeyDown={(e) => e.key === 'Enter' && retrieve()}
            />
            <div className="s1-sites-note">
              For: {enabledSites.map((x) => x.name || x.key).join(', ')}
            </div>
          </div>
        ) : (
          enabledSites.map((site) => (
            <div key={site.key} className="s1-cred-block">
              <div className="s1-cred-site">{site.name || site.key}</div>
              <input
                className="s1-input"
                placeholder="Username"
                {...noAutofill}
                value={credFor(site.key).username}
                onChange={(e) => setCredFor(site.key, { username: e.target.value })}
              />
              <input
                className="s1-input"
                type="password"
                placeholder="Password"
                {...noAutofill}
                value={credFor(site.key).password}
                onChange={(e) => setCredFor(site.key, { password: e.target.value })}
              />
            </div>
          ))
        )}

        {/* Save-login controls (task 4). */}
        {vaultPresent ? (
          <div className="s1-save-panel">
            <div className="s1-lock-note">🔒 Login saved on this device — retrieving will update it.</div>
            <button className="s1-forget" onClick={doForget}>Forget saved login</button>
          </div>
        ) : (
          <>
            <label className="s1-check">
              <input
                type="checkbox"
                checked={saveOn}
                onChange={(e) => { setSaveOn(e.target.checked); if (!e.target.checked) setConsent(false) }}
              />
              Save my login on this device
            </label>
            {saveOn && (
              <div className="s1-save-panel">
                <div className="s1-warn">
                  ⚠ Your login will be encrypted with a master passphrase and stored only in this
                  browser on your own disk — never on a server. We do our best to protect it, but no
                  storage is perfectly safe: a breach of your device is always possible, and you
                  accept that PerchBoard is not responsible for any resulting loss. The passphrase is
                  never stored; if you forget it, the saved login can't be recovered (just re-enter
                  and save again).
                </div>
                <label className="s1-check">
                  <input type="checkbox" checked={consent} onChange={(e) => setConsent(e.target.checked)} />
                  I understand and accept the risk
                </label>
                {consent && (
                  <>
                    <input
                      className="s1-input"
                      type="password"
                      placeholder="Set a master passphrase (6+ characters)"
                      {...noAutofill}
                      value={passphrase}
                      onChange={(e) => setPassphrase(e.target.value)}
                    />
                    <input
                      className="s1-input"
                      type="password"
                      placeholder="Confirm master passphrase"
                      {...noAutofill}
                      value={pass2}
                      onChange={(e) => setPass2(e.target.value)}
                    />
                  </>
                )}
              </div>
            )}
          </>
        )}

        {vaultMsg && <div className="muted-note" style={{ marginTop: 6 }}>{vaultMsg}</div>}
        {err && <div className="err-note" style={{ marginTop: 8 }}>{err}</div>}

        <div className="s1-actions">
          <button className="btn primary" onClick={() => retrieve()} disabled={!enabledSites.length}>
            Retrieve
          </button>
          {hasResults && (
            <button className="btn" onClick={() => setEditingCreds(false)}>
              Cancel
            </button>
          )}
        </div>

        <div className="s1-privacy">
          🔒 Credentials go only to your local PerchBoard to log in.{' '}
          {saveOn
            ? 'Your saved login is encrypted on this device with your master passphrase.'
            : 'They are not stored unless you tick “Save my login” above.'}
        </div>
      </div>
    )
  }

  // --- results --------------------------------------------------------------

  return (
    <div className="s1">
      <div className="s1-head">
        <div className="s1-title">Submission &amp; review status</div>
        <div>
          <button
            className="head-btn"
            title="Retrieve again"
            onClick={() => {
              setErr('')
              setVaultMsg('')
              // Go to the form. For a saved login this is the passphrase prompt —
              // required for every retrieval, not just once per session.
              setEditingCreds(true)
            }}
          >
            ↻
          </button>
          <button className="head-btn" onClick={() => setShowSettings(true)}>
            ⚙
          </button>
        </div>
      </div>

      {(() => {
        const active = results.find((r) => r.key === activeKey) || results[0]
        if (!active) return null
        const cur = view[active.key] || 'papers'
        return (
          <div className="s1-results">
            {/* One tab per journal. */}
            <div className="s1-jtabs">
              {results.map((r) => (
                <button
                  key={r.key}
                  className={'s1-jtab' + (r.key === active.key ? ' active' : '')}
                  onClick={() => setActiveKey(r.key)}
                >
                  {r.name || r.key}
                  {r.error ? ' ⚠' : ''}
                </button>
              ))}
            </div>

            <div className="s1-section">
              {active.error ? (
                <div className="err-note">⚠ {active.error}</div>
              ) : (
                <>
                  <div className="s1-toggle">
                    <button
                      className={'s1-tab' + (cur === 'papers' ? ' active' : '')}
                      onClick={() => setView({ ...view, [active.key]: 'papers' })}
                    >
                      Paper
                      {paperBlocks(active.papers).length
                        ? ` (${paperBlocks(active.papers).length})`
                        : ''}
                    </button>
                    <button
                      className={'s1-tab' + (cur === 'reviews' ? ' active' : '')}
                      onClick={() => setView({ ...view, [active.key]: 'reviews' })}
                    >
                      Review{(active.reviews || []).length ? ` (${active.reviews.length})` : ''}
                    </button>
                  </div>

                  {cur === 'papers' ? (
                    <PaperBlocks papers={active.papers} note={active.paperError} />
                  ) : (
                    <ReviewList reviews={active.reviews} note={active.reviewError} />
                  )}
                </>
              )}
            </div>
          </div>
        )
      })()}

      {retrievedAt && (
        <div className="s1-footer">Last retrieved {fmtTime(retrievedAt)}</div>
      )}
    </div>
  )
}

// splitId separates a manuscript ID into the base (the part shared across
// revisions) and its revision number. Revisions are suffixed ".R1", ".R2", … —
// e.g. "ISRE-2025-2185.R2" → base "ISRE-2025-2185", rev 2. The suffix is NOT
// removed from the displayed ID; it's only used to group the versions together.
function splitId(id) {
  const s = (id || '').trim()
  const m = s.match(/^(.*?)\.R(\d+)\s*$/i)
  if (m) return { base: m[1], rev: parseInt(m[2], 10) }
  return { base: s, rev: 0 }
}

// paperBlocks groups the Author rows by base ID so every revision of one
// manuscript (ISRE-…-2185, .R1, .R2) lands in a single block, ordered oldest →
// newest. Grouping is by base ID only — never by title, which can change across
// revisions. Each block keeps the most recent revision's title as its name.
function paperBlocks(papers) {
  const order = []
  const map = new Map()
  ;(papers || []).forEach((p) => {
    const { base, rev } = splitId(p.id)
    const key = base || p.id || '(no id)'
    if (!map.has(key)) {
      map.set(key, { base: key, versions: [] })
      order.push(key)
    }
    map.get(key).versions.push({ ...p, _rev: rev })
  })
  return order.map((key) => {
    const blk = map.get(key)
    blk.versions.sort((a, b) => a._rev - b._rev)
    const latest = blk.versions[blk.versions.length - 1]
    // Prefer the newest revision's title, but fall back to any non-empty one.
    blk.title =
      latest.title ||
      [...blk.versions].reverse().map((v) => v.title).find(Boolean) ||
      '(untitled submission)'
    return blk
  })
}

// PaperBlocks renders one block per manuscript, each listing all its revisions'
// progress oldest → newest, or a fallback note.
function PaperBlocks({ papers, note }) {
  const blocks = paperBlocks(papers)
  if (!blocks.length) {
    return <div className="s1-empty">{note || 'No paper information.'}</div>
  }
  return (
    <div className="s1-list">
      {blocks.map((blk) => (
        <div key={blk.base} className="s1-block">
          <div className="s1-block-title">{blk.title}</div>
          <div className="s1-block-base">
            {blk.base}
            {blk.versions.length > 1 && (
              <span className="s1-vcount"> · {blk.versions.length} versions</span>
            )}
          </div>
          {blk.versions.map((p, i) => (
            <div key={i} className="s1-version">
              <div className="s1-paper-row">
                {p.id && <span className="s1-id">{p.id}</span>}
                {p.status && <span className="s1-badge">{p.status}</span>}
              </div>
              {p.editors && p.editors.length > 0 && (
                <div className="s1-editors">{p.editors.join(' · ')}</div>
              )}
              {(p.submittingAuthor || p.created || p.submitted) && (
                <div className="s1-dates">
                  {p.submittingAuthor && <>Submitting author: {p.submittingAuthor} · </>}
                  {p.created && <>Created {p.created}</>}
                  {p.submitted && <> · Submitted {p.submitted}</>}
                </div>
              )}
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

// ReviewList renders the Reviewer dashboard rows generically (column headers are
// preserved as labels), or a fallback note.
function ReviewList({ reviews, note }) {
  if (!reviews || !reviews.length) {
    return <div className="s1-empty">{note || 'No review information.'}</div>
  }
  return (
    <div className="s1-list">
      {reviews.map((r, i) => (
        <div key={i} className="s1-review">
          {(r.columns || [])
            .filter((c) => c.value)
            .map((c, j) => (
              <div key={j} className="s1-col">
                {c.label && <span className="s1-col-label">{c.label}</span>}
                <span className="s1-col-val">{c.value}</span>
              </div>
            ))}
        </div>
      ))}
    </div>
  )
}

// fmtTime turns an ISO timestamp into a short local "today / date + time" label.
function fmtTime(iso) {
  try {
    const d = new Date(iso)
    const now = new Date()
    const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    if (d.toDateString() === now.toDateString()) return `today ${time}`
    return `${d.toLocaleDateString()} ${time}`
  } catch {
    return ''
  }
}
