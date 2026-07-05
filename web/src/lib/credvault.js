// credvault.js — encrypt/decrypt ScholarOne credentials in the browser, locked by
// a user master passphrase.
//
// Nothing here ever reaches the server. The encrypted blob lives in localStorage
// (the user's own browser profile on their disk), so it stays out of the server
// config and the Docker volume — a PerchBoard server-side update can't read it at
// rest. The passphrase itself is NEVER stored; callers hold it in memory only for
// the current session.
//
// Scheme: PBKDF2-SHA256 (210k iterations) derives an AES-256-GCM key from the
// passphrase + a random per-vault salt. AES-GCM is authenticated, so a wrong
// passphrase fails to decrypt (throws) instead of returning garbage — that's how
// we detect a bad passphrase.

const KEY_PREFIX = 'perchboard.s1vault.'
const ITERATIONS = 210000

const enc = new TextEncoder()
const dec = new TextDecoder()

// Small base64 helpers (the payloads are tiny, so the spread is safe).
const toB64 = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf)))
const fromB64 = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0))

async function deriveKey(passphrase, salt) {
  const base = await crypto.subtle.importKey('raw', enc.encode(passphrase), 'PBKDF2', false, ['deriveKey'])
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt, iterations: ITERATIONS, hash: 'SHA-256' },
    base,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  )
}

// Is there a saved vault for this widget?
export function hasVault(widgetId) {
  try { return localStorage.getItem(KEY_PREFIX + widgetId) != null } catch { return false }
}

// Delete the saved vault.
export function clearVault(widgetId) {
  try { localStorage.removeItem(KEY_PREFIX + widgetId) } catch { /* ignore */ }
}

// Encrypt `data` (any JSON-serialisable object) under the passphrase and persist
// it. Overwrites any existing vault for this widget.
export async function saveVault(widgetId, passphrase, data) {
  const salt = crypto.getRandomValues(new Uint8Array(16))
  const iv = crypto.getRandomValues(new Uint8Array(12))
  const key = await deriveKey(passphrase, salt)
  const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, enc.encode(JSON.stringify(data)))
  const blob = { v: 1, salt: toB64(salt), iv: toB64(iv), ct: toB64(ct) }
  localStorage.setItem(KEY_PREFIX + widgetId, JSON.stringify(blob))
}

// Decrypt the saved vault. Throws 'Wrong passphrase.' on a bad passphrase (or if
// the stored blob was tampered with), 'No saved login.' if nothing is stored.
export async function openVault(widgetId, passphrase) {
  const raw = localStorage.getItem(KEY_PREFIX + widgetId)
  if (!raw) throw new Error('No saved login.')
  const blob = JSON.parse(raw)
  const key = await deriveKey(passphrase, fromB64(blob.salt))
  let plain
  try {
    plain = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: fromB64(blob.iv) }, key, fromB64(blob.ct))
  } catch {
    throw new Error('Wrong passphrase.')
  }
  return JSON.parse(dec.decode(plain))
}
