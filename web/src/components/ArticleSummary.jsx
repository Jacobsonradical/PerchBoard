import React, { useEffect, useState } from 'react'
import { requestSummary } from '../lib/summary.js'

export default function ArticleSummary({ article, busy, sourceURL, allowPaste = false }) {
  const [closed, setClosed] = useState(false)
  const [result, setResult] = useState(null)
  const [error, setError] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [pastedArticle, setPastedArticle] = useState(null)
  const [draft, setDraft] = useState('')
  const [pasteError, setPasteError] = useState('')
  const summaryArticle = article || pastedArticle

  useEffect(() => { setClosed(false); setResult(null); setError(''); setAttempt(0); setPastedArticle(null); setDraft(''); setPasteError('') }, [sourceURL])
  useEffect(() => {
    if (!summaryArticle) return
    let active = true
    setResult(null); setError('')
    requestSummary(summaryArticle).then((data) => { if (active) setResult(data) })
      .catch((err) => { if (active) setError(err.message) })
    return () => { active = false }
  }, [summaryArticle, attempt])

  function summarizePaste(event) {
    event.preventDefault()
    const paragraphs = draft.split(/\n\s*\n/).map((text) => text.trim()).filter(Boolean)
    const length = [...paragraphs.join(' ')].length
    if (length < 600 || length > 60000 || paragraphs.length > 2000) {
      setPasteError('Paste 600–60,000 characters of article text, with no more than 2,000 paragraphs.')
      return
    }
    setPasteError('')
    setPastedArticle({ title: '', paragraphs })
    setDraft('')
  }

  if (closed) return <div className="article-summary-reopen"><button onClick={() => setClosed(false)}>✦ Show AI summary</button></div>
  const unavailable = !busy && !summaryArticle
  return <aside className={`article-summary${allowPaste && !pastedArticle ? ' article-summary-with-paste' : ''}`} aria-label="AI article summary">
    <div className="article-summary-heading">
      <div><span className="article-summary-star" aria-hidden="true">✦</span><strong>At a glance</strong><span className="article-summary-label">AI summary</span></div>
      <button className="article-summary-close" onClick={() => setClosed(true)} aria-label="Close AI summary">×</button>
    </div>
    <div aria-live="polite" aria-busy={!result && !error && !unavailable}>
      {result ? <p className="article-summary-text">{result.sentences.join(' ')}</p>
        : error ? <p className="article-summary-message">{error} <button onClick={() => setAttempt((value) => value + 1)}>Try again</button></p>
          : unavailable ? <p className="article-summary-message">No readable article text was available, so a summary could not be generated.</p>
            : <div className="article-summary-loading"><p>{summaryArticle ? 'Reading the article and finding the key points…' : 'Waiting for the article text…'}</p><span /><span /></div>}
    </div>
    {allowPaste && !pastedArticle && <form className="article-summary-paste" onSubmit={summarizePaste}>
      <label htmlFor="summary-pasted-text">Paste article text from Archive.today</label>
      <textarea id="summary-pasted-text" value={draft} onChange={(event) => setDraft(event.target.value)} rows={5} placeholder="Copy the article body from the Archive tab and paste it here." />
      <p>This text will be sent to your configured summary provider when you click Summarize pasted text. API charges may apply.</p>
      {pasteError && <p role="alert">{pasteError}</p>}
      <button type="submit">Summarize pasted text</button>
    </form>}
    {pastedArticle && <p className="article-summary-message">Using the article text you pasted. <button onClick={() => { setPastedArticle(null); setResult(null); setError('') }}>Replace pasted text</button></p>}
    <div className="article-summary-footnote">{result ? `${result.provider === 'openai' ? 'OpenAI' : 'Claude'} · ${result.model} · Based on ${pastedArticle && !article ? 'pasted' : 'extracted'} text; may contain errors.` : 'A brief overview in the article’s language.'}</div>
  </aside>
}
