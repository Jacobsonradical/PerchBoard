import test from 'node:test'
import assert from 'node:assert/strict'
import { requestSummary } from './summary.js'

test('coalesces simultaneous summary requests without retaining stale completed results', async () => {
  const previous = globalThis.fetch
  let finish, calls = 0
  globalThis.fetch = async () => { calls++; await new Promise((resolve) => { finish = resolve }); return { ok: true, json: async () => ({ sentences: ['A happened.', 'B followed.'] }) } }
  try {
    const article = { title: 'Story', paragraphs: ['Actual article text'] }
    const a = requestSummary(article), b = requestSummary(article)
    assert.equal(a, b)
    assert.equal(calls, 1)
    finish(); await a
    const c = requestSummary(article)
    assert.equal(calls, 2)
    finish(); await c
  } finally { globalThis.fetch = previous }
})

test('failed calls can be retried and malformed output is rejected', async () => {
  const previous = globalThis.fetch
  let calls = 0
  globalThis.fetch = async () => ({ ok: ++calls > 1, json: async () => calls === 1 ? { error: 'No summary key configured.' } : { sentences: ['Only one sentence.'] } })
  try {
    const article = { title: 'Retry', paragraphs: ['Text'] }
    await assert.rejects(requestSummary(article), /No summary key/)
    await assert.rejects(requestSummary(article), /invalid summary/)
    assert.equal(calls, 2)
  } finally { globalThis.fetch = previous }
})
