import React, { useEffect, useState } from 'react'
import ArticleSummary from './ArticleSummary'

const names = { local: 'PerchBoard reader', archive: 'Archive.today' }

export default function ArticleReader() {
  const [state, setState] = useState({ steps: [], article: null, busy: true })
  const params = new URLSearchParams(window.location.hash.slice(1))
  const raw = params.get('url') || ''
  let original = ''
  try { const u = new URL(raw); if (['http:', 'https:'].includes(u.protocol) && !u.username && !u.password) original = u.href } catch { /* Invalid links are displayed below. */ }
  const summary = params.get('summary') === '1'
  const local = params.get('local') === '1' || summary
  const archive = params.get('archive') === '1'

  useEffect(() => {
    document.body.classList.add('reader-page')
    return () => document.body.classList.remove('reader-page')
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    async function read() {
      const steps = []
      if (!original || (!local && !archive)) {
        setState({ steps: [], article: null, busy: false, error: 'No reading service selected or the article URL is invalid.' })
        return
      }
      const services = [local && 'local', archive && 'archive'].filter(Boolean)
      for (const service of services) {
        const fallback = (service === 'archive' && local)
        steps.push({ service, status: 'loading', message: `${fallback ? 'Falling back to ' : 'Trying '}${names[service]}…` })
        setState({ steps: [...steps], article: null, busy: true })
        if (service === 'archive') {
          const externalURL = `https://archive.ph/newest/${original}`
          steps[steps.length - 1] = { service, status: 'external', message: `${fallback ? 'PerchBoard could not read this article. Falling back to' : 'Opening'} Archive.today directly in your browser…` }
          setState({ steps: [...steps], article: null, busy: false, externalURL })
          return
        }
        try {
          const q = new URLSearchParams({ url: original, service })
          const response = await fetch(`/api/reader?${q}`, { signal: controller.signal })
          const article = await response.json()
          if (!response.ok) throw new Error(article.error || `HTTP ${response.status}`)
          if (!Array.isArray(article.paragraphs) || !article.paragraphs.length) throw new Error('No readable article returned.')
          steps[steps.length - 1] = { service, status: 'success', message: `Reading with ${names[service]}${fallback ? ' — fallback from PerchBoard reader' : ''}.` }
          setState({ steps: [...steps], article, busy: false })
          return
        } catch (err) {
          if (controller.signal.aborted) return
          steps[steps.length - 1] = { service, status: 'failed', message: `${names[service]} could not read this article: ${err.message}` }
        }
      }
      setState({ steps: [...steps], article: null, busy: false, error: 'Unable to read this article with the selected services.' })
    }
    read()
    return () => controller.abort()
  }, [original, local, archive])

  useEffect(() => {
    if (!state.externalURL) return
    const timer = setTimeout(() => window.location.assign(state.externalURL), 1800)
    return () => clearTimeout(timer)
  }, [state.externalURL])

  const article = state.article
  const publisher = article?.publisher || (original ? new URL(original).hostname.replace(/^www\./, '') : '')
  let published = article?.published || ''
  if (published && !Number.isNaN(Date.parse(published))) published = new Date(published).toLocaleDateString(undefined, { year: 'numeric', month: 'long', day: 'numeric' })
  const archiveURL = original ? `https://archive.ph/newest/${original}` : ''

  return (
    <main className="article-reader">
      <nav className="reader-toolbar" aria-label="Reader navigation">
        <a href="/">← PerchBoard</a>
        <div>
          {archive && archiveURL && <a href={archiveURL} target="_blank" rel="noreferrer">Open Archive.today ↗</a>}
          {original && <a href={original} target="_blank" rel="noreferrer">Original article ↗</a>}
        </div>
      </nav>
      {summary && <ArticleSummary key={original} article={state.article} busy={state.busy} sourceURL={original} />}
      <div className="reader-masthead">{publisher || 'Article reader'}</div>
      <div className="reader-status" role="status" aria-live="polite" aria-busy={state.busy}>
        {state.steps.map((step) => <p key={step.service} className={`reader-${step.status}`}>{step.message}</p>)}
        {state.error && <p>{state.error}</p>}
      </div>
      {state.externalURL && <p className="reader-handoff">
        <a href={state.externalURL}>Continue to Archive.today →</a><br />
        Archive.today will show the available snapshot or its own error. PerchBoard cannot verify the result after you leave this page.
      </p>}
      <header className="reader-header">
        <h1>{article?.title || 'Article reader'}</h1>
        {article?.description && <p className="reader-deck">{article.description}</p>}
        {(article?.author || published) && <div className="reader-byline">
          {article?.author && <span>By {article.author.replace(/,(?=\S)/g, ', ')}</span>}
          {published && <time dateTime={article.published}>{published}</time>}
        </div>}
      </header>
      {article && <>
        {safeResource(article.image) && <figure className="reader-hero"><img src={safeResource(article.image)} alt="" referrerPolicy="no-referrer" onError={(e) => { e.currentTarget.hidden = true }} /></figure>}
        <article className="reader-body">
          {article.content?.length ? <ArticleContent nodes={article.content} /> : article.paragraphs.map((text, i) => <p key={i}>{text}</p>)}
        </article>
        <footer className="reader-footer">Read with {names[article.service] || 'PerchBoard reader'}. Layout adapted from the original; article completeness could not be verified.</footer>
      </>}
      {!state.busy && !article && !state.externalURL && archive && original && <p className="reader-unavailable">
        Archive.today may block automated requests or need browser verification. <a href={archiveURL} target="_blank" rel="noreferrer">Open Archive.today directly ↗</a>. If no readable copy is available there, this article cannot be read through these services.
      </p>}
    </main>
  )
}

const allowedTags = new Set(['p', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'blockquote', 'pre', 'code', 'strong', 'em', 'b', 'i', 's', 'sup', 'sub', 'figure', 'figcaption', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'br', 'hr'])

function safeResource(value) {
  try {
    const u = new URL(value)
    return ['http:', 'https:'].includes(u.protocol) && !u.username && !u.password ? u.href : undefined
  } catch { return undefined }
}

// Only these elements and props can reach the DOM; publisher event handlers,
// style rules and raw markup are never rendered.
function ArticleContent({ nodes }) {
  return nodes.map((node, i) => {
    if (!node.tag) return node.text || null
    if (node.tag === 'img') {
      const src = safeResource(node.url)
      return src ? <img key={i} src={src} alt={node.alt || ''} loading="lazy" referrerPolicy="no-referrer" onError={(e) => { e.currentTarget.hidden = true }} /> : null
    }
    const children = <ArticleContent nodes={node.children || []} />
    if (node.tag === 'a') return <a key={i} href={safeResource(node.url)} target="_blank" rel="noreferrer">{children}</a>
    if (!allowedTags.has(node.tag)) return null
    if (node.tag === 'br' || node.tag === 'hr') return React.createElement(node.tag, { key: i })
    if (node.tag === 'table') return <div key={i} className="reader-table"><table>{children}</table></div>
    return React.createElement(node.tag, { key: i }, children)
  })
}
