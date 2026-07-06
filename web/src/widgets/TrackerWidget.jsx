import React, { useState, useEffect } from 'react'
import { api } from '../lib/api'
import { hasVault, clearVault, saveVault, openVault } from '../lib/credvault'

// TrackerWidget (widget type "scholarone", key kept so saved configs survive):
// on-demand retrieval of paper and review status across manuscript-system
// sites. Each site names its system — ScholarOne journals (ISR, Management
// Science, MISQ, …) or PCS conferences (e.g. ICIS) — and the local backend
// drives a headless browser to log in and read that system's dashboards.
// Credentials live only in this component's memory for the duration of a
// retrieval — never in the dashboard config.
//
// Two modes for results caching:
//  - Default (no saved login): scraped results are cached in widget.state so they
//    survive a reload until the next retrieval.
//  - Saved login (task 4): the user has an encrypted, passphrase-locked login. Then
//    results are kept in MEMORY only for the session — nothing sensitive is written
//    to disk, and after a reload the widget shows only the unlock screen until the
//    master passphrase is entered.
// The systems the tracker can drive, shown as top-level tabs. ScholarOne is a
// family of per-journal sites (defaults below, more addable in ⚙); PCS and
// PaperFox are each a single portal with a fixed URL, so their site entry is
// built in — the user only ever types a login.
const SYSTEMS = [
  { id: 'scholarone', label: 'ScholarOne', hint: 'e.g., ISR, Management Science, MISQ' },
  {
    id: 'pcs', label: 'PCS', hint: 'e.g., ICIS',
    site: { key: 'pcs', name: 'PCS', url: 'https://new.precisionconference.com/user/login', system: 'pcs' },
  },
  {
    id: 'paperfox', label: 'PaperFox', hint: 'e.g., CIST',
    site: { key: 'paperfox', name: 'PaperFox', url: 'https://www.paperfox.ai/signin', system: 'paperfox' },
  },
]

export default function TrackerWidget({ widget, onChange }) {
  const s = widget.settings || {}
  const st = widget.state || { results: [], retrievedAt: '' }
  // Settings hold only the ScholarOne journal list; PCS/PaperFox are implicit
  // (old configs may still carry their entries — ignored here).
  const isS1 = (site) => (site.system || 'scholarone') === 'scholarone'
  const journals = (s.sites || []).filter(isS1)
  const enabledJournals = journals.filter((x) => x.enabled !== false)
  const sameCreds = s.sameCreds !== false

  // Which system's results are being VIEWED — the tabs only switch the view.
  // Retrieval always covers every system that has a login (one Retrieve / ↻
  // refreshes everything at once).
  const [activeSystem, setActiveSystem] = useState('scholarone')
  const sitesFor = (sysId) => {
    if (sysId === 'scholarone') return enabledJournals
    const def = SYSTEMS.find((x) => x.id === sysId)
    return def && def.site ? [def.site] : []
  }

  const [showSettings, setShowSettings] = useState(false)
  // Settings are a two-step wizard: 1 = sites (what to track), 2 = logins.
  // Every visit to ⚙ walks the same two steps.
  const [settingsStep, setSettingsStep] = useState(1)
  const openSettings = () => { setSettingsStep(1); setShowSettings(true) }
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
      // Vault logins, with anything typed in settings this session on top (so
      // a just-added PCS/PaperFox login joins the retrieval before re-saving).
      const ps = { ...(v.perSite || {}) }
      for (const [k, c] of Object.entries(perSite)) {
        if (credOk(c)) ps[k] = c
      }
      retrieve({
        shared: credOk(shared) ? shared : (v.shared || { username: '', password: '' }),
        perSite: ps,
      })
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
  const allResults = protectedMode ? (sessionResults || []) : (st.results || [])
  const retrievedAt = protectedMode ? sessionRetrievedAt : (st.retrievedAt || '')
  // Each cached result is tagged with its system; older caches predate the tag
  // and were ScholarOne-only. The tabs switch between systems that have results.
  const resultSystem = (r) => r.system || 'scholarone'
  const hasResults = allResults.length > 0
  const showForm = !hasResults || editingCreds

  // A system takes part in a retrieval when its login is filled in (in ⚙
  // settings, this session) — a blank login just skips that system. A saved
  // vault may hold more logins than the session has; retrieval merges them in.
  const credOk = (c) => c && (c.username || '').trim() && c.password
  const sessionCredsFor = (site) =>
    isS1(site) && sameCreds ? shared : (perSite[site.key] || { username: '', password: '' })
  const systemReady = (sysId) => sitesFor(sysId).some((site) => credOk(sessionCredsFor(site)))
  const anyReady = SYSTEMS.some((sys) => systemReady(sys.id))

  // --- helpers --------------------------------------------------------------

  const update = (patch) => onChange({ ...widget, ...patch })
  const setSites = (next) => update({ settings: { ...s, sites: next } })

  const credFor = (key) => perSite[key] || { username: '', password: '' }
  const setCredFor = (key, patch) =>
    setPerSite((p) => ({ ...p, [key]: { ...credFor(key), ...patch } }))

  // Retrieves EVERY system whose login is available — one click refreshes
  // ScholarOne, PCS, and PaperFox together; a system without a login is simply
  // skipped. `ov` optionally supplies { shared, perSite } directly, used when
  // unlocking auto-retrieves (the decrypted creds aren't in state yet,
  // setState is async).
  const retrieve = async (ov) => {
    setErr('')
    const sh = ov?.shared || shared
    const ps = ov?.perSite || perSite
    const creds = []
    for (const sys of SYSTEMS) {
      for (const site of sitesFor(sys.id)) {
        // The shared login covers only the ScholarOne journals; PCS and
        // PaperFox are separate services with their own credentials.
        const c = isS1(site) && sameCreds ? sh : (ps[site.key] || { username: '', password: '' })
        if (!credOk(c)) continue
        creds.push({
          key: site.key,
          name: site.name,
          url: site.url,
          system: site.system || 'scholarone',
          username: (c.username || '').trim(),
          password: c.password || '',
        })
      }
    }
    if (!creds.length) {
      setErr('No logins set up yet — open settings (⚙) to enter them.')
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
      const res = await api.tracker(creds)
      const now = new Date().toISOString()
      // Results come back in creds order; tag each with its system. Systems
      // not part of this retrieval (no login this time) keep their old cache.
      const tagged = res.map((r, i) => ({ ...r, system: creds[i]?.system || 'scholarone' }))
      const retrievedSystems = new Set(creds.map((c) => c.system))
      const merged = [
        ...allResults.filter((r) => !retrievedSystems.has(resultSystem(r))),
        ...tagged,
      ]
      if (protectedMode) {
        // Keep protected results in memory only; never persist them to disk.
        setSessionResults(merged)
        setSessionRetrievedAt(now)
        if ((st.results && st.results.length) || st.retrievedAt) {
          update({ state: { results: [], retrievedAt: '' } })
        }
      } else {
        update({ state: { results: merged, retrievedAt: now } })
      }
      // Save the encrypted vault only when first creating it (a passphrase was just
      // set). An existing vault is never re-written on a normal retrieve.
      if (saveOn && !vaultPresent && passphrase) {
        try {
          await saveVault(widget.id, passphrase, { shared: sh, perSite: ps })
          setVaultPresent(true)
        } catch { /* saving is best-effort; a failure shouldn't lose the results */ }
      }
      setEditingCreds(false)
      setManual(false)
      setPass2('')
      setPassphrase('')
      // With a saved login, credentials are only ever decrypted transiently —
      // drop them (and the passphrase) after every retrieval so nothing stays
      // "unlocked". Without a vault the user typed them in settings for this
      // session; keep them so ↻ works without a trip back to settings.
      if (vaultPresent || saveOn) {
        setShared({ username: '', password: '' })
        setPerSite({})
      }
    } catch (e) {
      setErr(e.message || 'Retrieval failed.')
    } finally {
      setBusy(false)
    }
  }

  // --- settings -------------------------------------------------------------
  // Settings hold everything needed BEFORE a retrieval: the ScholarOne journal
  // list, the logins for each system (a blank login just skips that system),
  // and the optional encrypted save-login. The main view is then only
  // Retrieve → results.

  if (showSettings) {
    return (
      <div className="s1">
        <div className="s1-head">
          <div className="s1-title">
            {settingsStep === 1 ? 'Settings — step 1 of 2: sites' : 'Settings — step 2 of 2: logins'}
          </div>
        </div>

        {settingsStep === 1 && (
          <div className="s1-intro">
            <p>
              👋 <b>Please set up your ScholarOne journals first.</b> We start
              you with <b>ISR</b>, <b>Management Science</b>, and{' '}
              <b>MIS Quarterly</b> — add more journals below if you need them.
            </p>
            <p>
              <b>PCS</b> (e.g., ICIS) and <b>PaperFox</b> (e.g., CIST) are
              already included for you — nothing to set up there.
            </p>
            <p>When you're ready, your logins come on the next page.</p>
          </div>
        )}

        {settingsStep === 2 && (<>
        <div className="section">
          <div className="s1-label">Logins</div>
          <div className="muted-note" style={{ marginBottom: 8 }}>
            Retrieve covers every system with a login below; leave one blank to
            skip it. If your ScholarOne journals use different passwords, untick
            the shared-login box to enter one per journal.
            {vaultPresent ? ' Your saved login fills these automatically at retrieval.' : ''}
          </div>

          {/* ScholarOne — one account usually works across the journals. */}
          <div className="s1-cred-block" data-site="scholarone">
            <div className="s1-cred-site">ScholarOne (e.g., ISR, Management Science, MISQ)</div>
            {enabledJournals.length > 1 && (
              <label className="s1-check s1-samecreds">
                <input
                  type="checkbox"
                  checked={sameCreds}
                  onChange={(e) => update({ settings: { ...s, sameCreds: e.target.checked } })}
                />
                Use the same username &amp; password for every ScholarOne journal
              </label>
            )}
            {enabledJournals.length === 0 ? (
              <div className="muted-note">No journals enabled (see the list below).</div>
            ) : sameCreds ? (
              <>
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
                />
                <div className="s1-sites-note">
                  For: {enabledJournals.map((x) => x.name || x.key).join(', ')}
                </div>
              </>
            ) : (
              enabledJournals.map((site) => (
                <div key={site.key} style={{ marginBottom: 6 }}>
                  <div className="s1-sites-note">{site.name || site.key}</div>
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
          </div>

          {/* PCS / PaperFox — single built-in portals, only a login needed. */}
          {SYSTEMS.filter((sys) => sys.site).map((sys) => (
            <div key={sys.id} className="s1-cred-block" data-site={sys.site.key}>
              <div className="s1-cred-site">{sys.label} ({sys.hint})</div>
              <input
                className="s1-input"
                placeholder="Username or email"
                {...noAutofill}
                value={credFor(sys.site.key).username}
                onChange={(e) => setCredFor(sys.site.key, { username: e.target.value })}
              />
              <input
                className="s1-input"
                type="password"
                placeholder="Password"
                {...noAutofill}
                value={credFor(sys.site.key).password}
                onChange={(e) => setCredFor(sys.site.key, { password: e.target.value })}
              />
              <div className="s1-sites-note">{shortHost(sys.site.url)}</div>
            </div>
          ))}
        </div>

        <div className="section">
          {/* Save-login (encrypted vault) opt-in lives here with the logins. */}
          {vaultPresent ? (
            <div className="s1-save-panel">
              <div className="s1-lock-note">🔒 Login saved on this device — retrieving will use it.</div>
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
                      <div className="muted-note">The vault is written on your next successful retrieval.</div>
                    </>
                  )}
                </div>
              )}
            </>
          )}
        </div>
        </>)}

        {settingsStep === 1 && (
        <div className="section">
          <div className="s1-label">ScholarOne journals</div>
          <div className="feed-edit-list">
            {journals.map((site, i) => (
              <div key={i} className="s1-site-edit">
                <input
                  type="checkbox"
                  title="Include in retrieval"
                  checked={site.enabled !== false}
                  onChange={(e) => {
                    const next = [...journals]
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
                      const next = [...journals]
                      next[i] = { ...site, name: e.target.value }
                      setSites(next)
                    }}
                  />
                  <input
                    className="feed-name-input s1-url-input"
                    value={site.url}
                    placeholder="https://mc.manuscriptcentral.com/…"
                    onChange={(e) => {
                      const next = [...journals]
                      next[i] = { ...site, url: e.target.value }
                      setSites(next)
                    }}
                  />
                </div>
                <button
                  className="chip-x"
                  title="Remove journal"
                  onClick={() => setSites(journals.filter((_, j) => j !== i))}
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
              setSites([...journals, { key: 's' + Date.now(), name: '', url: '', system: 'scholarone', enabled: true }])
            }
          >
            + Add journal
          </button>
          <div className="muted-note" style={{ marginTop: 8 }}>
            PCS and PaperFox need no site setup — their portals are built in.
          </div>
        </div>
        )}

        {settingsStep === 2 && vaultMsg && (
          <div className="muted-note" style={{ marginTop: 6 }}>{vaultMsg}</div>
        )}

        {settingsStep === 2 && (
          <div className="s1-privacy">
            🔒 Credentials go only to your local PerchBoard to log in.{' '}
            {saveOn || vaultPresent
              ? 'Your saved login is encrypted on this device with your master passphrase.'
              : 'They are kept in memory for this session only, unless you tick “Save my login”.'}
          </div>
        )}

        <div className="s1-actions" style={{ marginTop: 10 }}>
          {settingsStep === 1 ? (
            <>
              <button className="btn primary" onClick={() => setSettingsStep(2)}>
                Next: logins →
              </button>
              <button className="btn" onClick={() => setShowSettings(false)}>Close</button>
            </>
          ) : (
            <>
              <button className="btn" onClick={() => setSettingsStep(1)}>← Back to sites</button>
              <button className="btn primary" onClick={() => setShowSettings(false)}>Done</button>
            </>
          )}
        </div>
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
          Logging in to each configured site in the background. This can take up
          to a minute.
        </div>
      </div>
    )
  }

  // --- pre-results screen ----------------------------------------------------
  // Credentials live in ⚙ settings; this screen only unlocks (saved login) or
  // fires the all-systems retrieval — plus a pointer to settings on first use.

  if (showForm) {
    // A saved login exists: ALWAYS ask for the master passphrase before a retrieval
    // (the passphrase is never cached for the session). Entering it retrieves right
    // away. "Use settings logins" allows a one-off retrieval without the vault.
    if (vaultPresent && !manual) {
      return (
        <div className="s1">
          <div className="s1-head">
            <div className="s1-title">Unlock saved login</div>
            <button className="head-btn" onClick={openSettings}>⚙</button>
          </div>
          <div className="s1-lock-note">
            🔒 Your login is saved and encrypted on this device. Enter your master
            passphrase to retrieve everything.
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
            {anyReady && (
              <button className="btn" onClick={() => setManual(true)}>
                Use settings logins
              </button>
            )}
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
          <button className="head-btn" onClick={openSettings}>
            ⚙
          </button>
        </div>

        {anyReady ? (
          <>
            {/* Which systems this retrieval will cover, from the settings logins. */}
            <div className="s1-lock-note">
              One click retrieves everything that has a login in settings:
            </div>
            <div className="s1-sysready">
              {SYSTEMS.map((sys) => (
                <span key={sys.id} className={'s1-badge' + (systemReady(sys.id) ? '' : ' off')}>
                  {systemReady(sys.id) ? '✓ ' : ''}{sys.label}{systemReady(sys.id) ? '' : ' — no login'}
                </span>
              ))}
            </div>
          </>
        ) : (
          <div className="s1-lock-note">
            👋 First time here? Open settings (⚙) and enter your logins for
            ScholarOne (e.g., ISR, MISQ), PCS (e.g., ICIS), and/or PaperFox
            (e.g., CIST) — leave blank any you don't use. You can also save
            them encrypted behind a master passphrase. Then come back and
            Retrieve.
          </div>
        )}

        {err && <div className="err-note" style={{ marginTop: 8 }}>{err}</div>}

        <div className="s1-actions">
          {anyReady ? (
            <button className="btn primary" onClick={() => retrieve()}>Retrieve</button>
          ) : (
            <button className="btn primary" onClick={openSettings}>Open settings</button>
          )}
          {hasResults && (
            <button className="btn" onClick={() => setEditingCreds(false)}>
              Cancel
            </button>
          )}
        </div>

        <div className="s1-privacy">
          🔒 Credentials go only to your local PerchBoard to log in. They are kept
          in memory for this session only, unless saved encrypted in settings.
        </div>
      </div>
    )
  }

  // --- results --------------------------------------------------------------

  // The system tabs only switch which retrieved system is being viewed.
  const systemsWithResults = SYSTEMS.filter((sys) =>
    allResults.some((r) => resultSystem(r) === sys.id))
  const viewSysId = systemsWithResults.some((x) => x.id === activeSystem)
    ? activeSystem
    : (systemsWithResults[0] || {}).id
  const viewResults = allResults.filter((r) => resultSystem(r) === viewSysId)

  return (
    <div className="s1">
      <div className="s1-head">
        <div className="s1-title">Submission &amp; review status</div>
        <div>
          <button
            className="head-btn"
            title="Retrieve everything again"
            onClick={() => {
              setErr('')
              setVaultMsg('')
              // Refresh covers ALL configured systems. With this-session logins
              // it fires straight away; a saved login goes through the
              // passphrase prompt (required for every retrieval).
              if (!vaultPresent && anyReady) retrieve()
              else setEditingCreds(true)
            }}
          >
            ↻
          </button>
          <button className="head-btn" onClick={openSettings}>
            ⚙
          </button>
        </div>
      </div>

      {/* Each system is its own page; this menu switches between them. Systems
          that weren't part of a retrieval yet are listed but not selectable. */}
      <div className="s1-mode-row">
        <select
          className="s1-mode-select"
          title="Switch system"
          value={viewSysId || ''}
          onChange={(e) => setActiveSystem(e.target.value)}
        >
          {SYSTEMS.map((sys) => {
            const has = systemsWithResults.some((x) => x.id === sys.id)
            return (
              <option key={sys.id} value={sys.id} disabled={!has}>
                {sys.label}{has ? '' : ' — not retrieved'}
              </option>
            )
          })}
        </select>
      </div>

      {(() => {
        const active = viewResults.find((r) => r.key === activeKey) || viewResults[0]
        if (!active) return null
        const cur = view[active.key] || 'papers'
        return (
          <div className="s1-results">
            {/* One tab per journal — only ScholarOne has several sites; a
                single-portal system needs no second tab row. */}
            {viewResults.length > 1 && (
              <div className="s1-jtabs">
                {viewResults.map((r) => (
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
            )}

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

// shortHost trims a URL down to its bare hostname for display.
function shortHost(u) {
  try {
    return new URL(u).hostname.replace(/^www\./, '')
  } catch {
    return u
  }
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
                {p.section && <span className="s1-badge">{p.section}</span>}
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
              {/* PCS submissions carry a track/category and the round's deadline. */}
              {(p.category || p.deadline) && (
                <div className="s1-dates">
                  {p.category}
                  {p.category && p.deadline && ' · '}
                  {p.deadline && <>Deadline {p.deadline}</>}
                </div>
              )}
              {p.note && <div className="s1-dates">Note: {p.note}</div>}
              {p.actions && p.actions.length > 0 && (
                <div className="s1-dates">On the site: {p.actions.join(' · ')}</div>
              )}
            </div>
          ))}
        </div>
      ))}
    </div>
  )
}

// daysUntil parses a ScholarOne due date ("14-Jul-2026") and returns the whole
// days from today (negative = overdue), or null if the format is unexpected.
function daysUntil(due) {
  const m = (due || '').match(/^(\d{1,2})-([A-Za-z]{3})-(\d{4})$/)
  if (!m) return null
  const months = { jan: 0, feb: 1, mar: 2, apr: 3, may: 4, jun: 5, jul: 6, aug: 7, sep: 8, oct: 9, nov: 10, dec: 11 }
  const mon = months[m[2].toLowerCase()]
  if (mon === undefined) return null
  const d = new Date(Number(m[3]), mon, Number(m[1]))
  return Math.round((d - new Date().setHours(0, 0, 0, 0)) / 86400000)
}

// ReviewRow renders one review. A row from the standard template is a card
// like the paper blocks: title, ID, status + due-date badges, the handling
// editors, the actions ScholarOne currently offers, and the abstract behind a
// click-to-expand. A row from an unrecognized site layout still renders as
// generic header→value lines. `done` suppresses the due-date urgency colors —
// a submitted review's past due date isn't "overdue".
function ReviewRow({ r, done }) {
  // Fallback shape: labelled columns only.
  if (!r.id && !r.title && !r.status && !r.dueDate) {
    return (
      <div className="s1-review">
        {(r.columns || [])
          .filter((c) => c.value)
          .map((c, j) => (
            <div key={j} className="s1-col">
              {c.label && <span className="s1-col-label">{c.label}</span>}
              <span className="s1-col-val">{c.value}</span>
            </div>
          ))}
      </div>
    )
  }
  const days = done ? null : daysUntil(r.dueDate)
  const dueClass = days === null ? '' : days < 0 ? ' overdue' : days <= 7 ? ' warn' : ''
  const dueNote =
    days === null ? '' : days < 0 ? ` · ${-days}d overdue` : days === 0 ? ' · due today' : ` · ${days}d left`
  return (
    <div className="s1-review">
      {r.title && <div className="s1-block-title">{r.title}</div>}
      <div className="s1-paper-row">
        {r.id && <span className="s1-id">{r.id}</span>}
        {r.status && <span className="s1-badge">{r.status}</span>}
        {r.dueDate && <span className={'s1-badge s1-due' + dueClass}>Due {r.dueDate}{dueNote}</span>}
        {r.completed && <span className="s1-badge">Completed {r.completed}</span>}
        {r.sent && <span className="s1-badge">Invited {r.sent}</span>}
        {r.type && <span className="s1-badge">{r.type}</span>}
      </div>
      {r.editors && r.editors.length > 0 && <div className="s1-editors">{r.editors.join(' · ')}</div>}
      {r.actions && r.actions.length > 0 && (
        <div className="s1-dates">On the site: {r.actions.join(' · ')}</div>
      )}
      {/* Any labelled cells the parser didn't recognize (queue columns vary per site). */}
      {r.columns && r.columns.length > 0 && r.columns.map((c, j) => (
        <div key={j} className="s1-dates">{c.label}: {c.value}</div>
      ))}
      {r.abstract && (
        <details className="s1-abstract">
          <summary>Abstract</summary>
          <p>{r.abstract}</p>
        </details>
      )}
    </div>
  )
}

// ReviewList shows all reviewer queues, grouped and in retrieval order:
// the active "Review and Score" list first, then e.g. "Invitations", with
// "Scores Submitted" (the completed-review history, often long) collapsed
// behind a click-to-expand header. Results without queue names (older cached
// retrievals) render as one flat list, as before.
function ReviewList({ reviews, note }) {
  if (!reviews || !reviews.length) {
    return <div className="s1-empty">{note || 'No review information.'}</div>
  }
  // Group by queue, preserving first-seen order.
  const order = []
  const byQueue = new Map()
  for (const r of reviews) {
    const q = r.queue || ''
    if (!byQueue.has(q)) { byQueue.set(q, []); order.push(q) }
    byQueue.get(q).push(r)
  }
  return (
    <div className="s1-list">
      {note && <div className="s1-empty">⚠ {note}</div>}
      {order.map((q) => {
        const items = byQueue.get(q)
        const done = /submitted/i.test(q) // completed history: no urgency colors
        const rows = items.map((r, i) => <ReviewRow key={r.id || i} r={r} done={done} />)
        if (order.length === 1 && !q) return rows // legacy flat list
        if (done) {
          return (
            <details key={q} className="s1-queue">
              <summary className="s1-queue-head">{q || 'Reviews'} ({items.length})</summary>
              {rows}
            </details>
          )
        }
        return (
          <div key={q}>
            <div className="s1-queue-head">{q || 'Reviews'} ({items.length})</div>
            {rows}
          </div>
        )
      })}
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
