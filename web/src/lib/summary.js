// Share only in-flight requests so React remounts do not duplicate paid calls.
// Completed results are cached by the server with the current model and key.
const pending = new Map()

export function requestSummary(article) {
  const body = JSON.stringify({ title: article.title, paragraphs: article.paragraphs })
  if (pending.has(body)) return pending.get(body)
  const request = (async () => {
    const response = await fetch('/api/summary', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body })
    const data = await response.json().catch(() => ({}))
    if (!response.ok) throw new Error(data.error || 'Summary could not be generated.')
    if (!Array.isArray(data.sentences) || data.sentences.length < 2 || data.sentences.length > 3 || data.sentences.some((s) => typeof s !== 'string' || !s.trim())) {
      throw new Error('The model returned an invalid summary.')
    }
    return data
  })()
  pending.set(body, request)
  request.then(() => pending.delete(body), () => pending.delete(body))
  return request
}
