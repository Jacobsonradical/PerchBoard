import test from 'node:test'
import assert from 'node:assert/strict'
import { articleLink, normFeed } from './articleLink.js'

test('original by default and old feeds normalize safely', () => {
  const feed = normFeed('https://example.com/rss')
  assert.equal(feed.readerLocal, false)
  assert.equal(feed.readerArchive, false)
  assert.equal(articleLink({ link: 'https://example.com/a?x=1&b=2#part', feedUrl: feed.url }, [feed]), 'https://example.com/a?x=1&b=2#part')
})
test('each combination round trips and applies independently per feed', () => {
  for (const readerLocal of [false, true]) for (const readerArchive of [false, true]) {
    const feed = normFeed({ url: 'https://example.com/rss', readerLocal, readerArchive })
    const item = { link: 'https://example.com/a?q=a%26b&b=2#part', feedUrl: feed.url }
    const link = articleLink(item, [feed])
    if (!readerLocal && !readerArchive) assert.equal(link, item.link)
    else {
      const q = new URLSearchParams(link.split('#')[1])
      assert.equal(q.get('url'), item.link)
      assert.equal(q.get('local'), readerLocal ? '1' : '0')
      assert.equal(q.get('archive'), readerArchive ? '1' : '0')
    }
    assert.equal(articleLink({ ...item, feedUrl: 'another-feed' }, [feed]), item.link)
  }
})
test('legacy saves resolve by source and removed feeds use original', () => {
  const feed = normFeed({ url: 'rss', readerLocal: true })
  const saved = { link: 'https://example.com/a', source: 'Publisher' }
  assert.match(articleLink(saved, [feed], { Publisher: 'rss' }), /^\/read#/)
  assert.equal(articleLink(saved, [], { Publisher: 'rss' }), saved.link)
})
test('reject unsafe article links', () => {
  for (const link of ['javascript:alert(1)', 'data:text/html,x', 'https://u:p@example.com', 'invalid']) assert.equal(articleLink({ link }, []), undefined)
})
