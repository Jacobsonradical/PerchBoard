// Pretty notifications for new content (used by the RSS widget for new items).
// Two layers:
//   1. a native OS notification (if the user has granted permission), and
//   2. an in-app toast (always shown) so it works even without permission.

// requestNotifyPermission asks once; safe to call repeatedly.
export async function requestNotifyPermission() {
  if (!('Notification' in window)) return false
  if (Notification.permission === 'granted') return true
  if (Notification.permission === 'denied') return false
  const res = await Notification.requestPermission()
  return res === 'granted'
}

// --- in-app toast system ---------------------------------------------------
// A minimal pub/sub the App subscribes to render toasts. No dependency needed.
const listeners = new Set()
export function onToast(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

let toastId = 0
export function toast({ title, body, icon }) {
  const t = { id: ++toastId, title, body, icon }
  listeners.forEach((fn) => fn(t))

  // Mirror to a native notification when allowed.
  if ('Notification' in window && Notification.permission === 'granted') {
    try {
      new Notification(title, { body, icon })
    } catch {
      /* ignore (some platforms restrict constructor use) */
    }
  }
}
