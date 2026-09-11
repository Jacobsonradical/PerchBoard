import React, { useEffect, useState } from 'react'
import { requestSummary } from '../lib/summary.js'

export default function ArticleSummary({ article, busy, sourceURL }) {
  const [closed, setClosed] = useState(false)
  const [result, setResult] = useState(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => { setClosed(false); setResult(null); setError(''); setAttempt(0) }, [sourceURL])
  useEffect(() => {
    if (!article) return
    let active = true
    setResult(null); setError('')
    requestSummary(article).then((data) => { if (active) setResult(data) })
      .catch((err) => { if (active) setError(err.message) })
    return () => { active = false }
  }, [article, attempt])

  if (closed) return <div className="article-summary-reopen"><button onClick={() => setClosed(false)}>✦ Show AI summary</button></div>
  const unavailable = !busy && !article
  return <aside className="article-summary" aria-label="AI article summary">
    <div className="article-summary-heading">
      <div><span className="article-summary-star" aria-hidden="true">✦</span><strong>At a glance</strong><span className="article-summary-label">AI summary</span></div>
      <button className="article-summary-close" onClick={() => setClosed(true)} aria-label="Close AI summary">×</button>
    </div>
    <div aria-live="polite" aria-busy={!result && !error && !unavailable}>
      {result ? <p className="article-summary-text">{result.sentences.join(' ')}</p>
        : error ? <p className="article-summary-message">{error} <button onClick={() => setAttempt((value) => value + 1)}>Try again</button></p>
          : unavailable ? <p className="article-summary-message">No readable article text was available, so a summary could not be generated.</p>
            : <div className="article-summary-loading"><p>{article ? 'Reading the article and finding the key points…' : 'Waiting for the article text…'}</p><span /><span /></div>}
    </div>
    <div className="article-summary-footnote">{result ? `${result.provider === 'openai' ? 'OpenAI' : 'Claude'} · ${result.model} · Based on extracted text; may contain errors.` : 'A brief overview in the article’s language.'}</div>
  </aside>
}
