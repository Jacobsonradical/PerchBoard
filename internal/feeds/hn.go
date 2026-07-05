package feeds

import (
	"fmt"
	"sync"
	"time"
)

// HNItem is a single Hacker News story.
type HNItem struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`   // external link (may be empty for Ask/Show HN)
	By    string `json:"by"`
	Score int    `json:"score"`
	Descendants int `json:"comments"` // comment count
	Time  int64  `json:"time"`  // unix seconds
}

// hnCommentsURL is the canonical HN discussion page for a story id.
func hnCommentsURL(id int) string {
	return fmt.Sprintf("https://news.ycombinator.com/item?id=%d", id)
}

// FetchHN returns the top `limit` Hacker News stories using the official
// Firebase API. Story details are fetched concurrently. Cached for 5 minutes.
func FetchHN(limit int) ([]HNItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	key := fmt.Sprintf("hn:%d", limit)
	v, err := cached(key, 5*time.Minute, func() (any, error) {
		var ids []int
		if err := getJSON("https://hacker-news.firebaseio.com/v0/topstories.json", &ids); err != nil {
			return nil, err
		}
		if len(ids) > limit {
			ids = ids[:limit]
		}

		// Fetch each story concurrently; preserve rank order in the result.
		items := make([]HNItem, len(ids))
		var wg sync.WaitGroup
		for i, id := range ids {
			wg.Add(1)
			go func(i, id int) {
				defer wg.Done()
				var raw struct {
					ID          int    `json:"id"`
					Title       string `json:"title"`
					URL         string `json:"url"`
					By          string `json:"by"`
					Score       int    `json:"score"`
					Descendants int    `json:"descendants"`
					Time        int64  `json:"time"`
				}
				url := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id)
				if err := getJSON(url, &raw); err != nil {
					return // leave a zero item; filtered out below
				}
				link := raw.URL
				if link == "" {
					link = hnCommentsURL(raw.ID) // Ask/Show HN: point at the thread
				}
				items[i] = HNItem{
					ID: raw.ID, Title: raw.Title, URL: link, By: raw.By,
					Score: raw.Score, Descendants: raw.Descendants, Time: raw.Time,
				}
			}(i, id)
		}
		wg.Wait()

		// Drop any that failed to load (zero ID).
		out := items[:0]
		for _, it := range items {
			if it.ID != 0 {
				out = append(out, it)
			}
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]HNItem), nil
}
