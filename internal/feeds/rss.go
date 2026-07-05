package feeds

import (
	"context"
	"sort"
	"time"

	"github.com/mmcdole/gofeed"
)

// RSSItem is the normalized shape a feed item takes on its way to the frontend.
// A stable GUID lets the frontend track read/ignored state across refreshes.
type RSSItem struct {
	GUID      string `json:"guid"`
	Title     string `json:"title"`
	Link      string `json:"link"`
	Source    string `json:"source"`    // feed/site title
	Published string `json:"published"` // RFC3339, empty if unknown
}

// FetchRSS downloads and parses one feed URL into normalized items, newest
// first. Results are cached for 5 minutes per URL.
func FetchRSS(url string) ([]RSSItem, error) {
	v, err := cached("rss:"+url, 5*time.Minute, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()

		fp := gofeed.NewParser()
		fp.UserAgent = UserAgent
		feed, err := fp.ParseURLWithContext(url, ctx)
		if err != nil {
			return nil, err
		}

		items := make([]RSSItem, 0, len(feed.Items))
		for _, it := range feed.Items {
			published := ""
			if it.PublishedParsed != nil {
				published = it.PublishedParsed.Format(time.RFC3339)
			} else if it.UpdatedParsed != nil {
				published = it.UpdatedParsed.Format(time.RFC3339)
			}
			// Prefer an explicit GUID; fall back to the link so the frontend
			// always has a stable key for read/ignore tracking.
			guid := it.GUID
			if guid == "" {
				guid = it.Link
			}
			items = append(items, RSSItem{
				GUID:      guid,
				Title:     it.Title,
				Link:      it.Link,
				Source:    feed.Title,
				Published: published,
			})
		}
		// Newest first; items without a date sink to the bottom.
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Published > items[j].Published
		})
		return items, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]RSSItem), nil
}
