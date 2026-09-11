export const normFeed = (feed) => typeof feed === 'string'
  ? { url: feed, name: '', readerLocal: false, readerArchive: false }
  : { ...feed, name: feed.name || '', readerLocal: !!feed.readerLocal, readerArchive: !!feed.readerArchive }

export function articleLink(item, feeds, sourceToFeedUrl = {}) {
  let original
  try {
    original = new URL(item.link)
    if (!['http:', 'https:'].includes(original.protocol) || original.username || original.password) return undefined
  } catch { return undefined }
  const feed = feeds.find((f) => f.url === (item.feedUrl || sourceToFeedUrl[item.source]))
  if (!feed?.readerLocal && !feed?.readerArchive) return original.href
  const params = new URLSearchParams({ url: original.href, local: feed.readerLocal ? '1' : '0', archive: feed.readerArchive ? '1' : '0' })
  return `/read#${params}`
}
