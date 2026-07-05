import { useEffect, useState, useRef, useCallback } from 'react'

// usePoll runs an async fetcher immediately and then every `intervalMs`,
// exposing { data, error, loading, refresh }. `deps` re-trigger an immediate
// fetch when they change (e.g. the user edits a widget's settings). The fetcher
// identity is intentionally NOT a dependency — pass settings via `deps`.
export function usePoll(fetcher, intervalMs, deps = []) {
  const [data, setData] = useState(null)
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(true)
  const fetcherRef = useRef(fetcher)
  fetcherRef.current = fetcher

  const run = useCallback(async () => {
    try {
      const d = await fetcherRef.current()
      setData(d)
      setError(null)
    } catch (e) {
      setError(e.message || String(e))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    setLoading(true)
    run()
    if (!intervalMs) return
    const id = setInterval(run, intervalMs)
    return () => clearInterval(id)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run, intervalMs, ...deps])

  return { data, error, loading, refresh: run }
}
