package feeds

import (
	"context"
	"net/url"
	"time"

	"github.com/mmcdole/gofeed"
)

// StockNews is one news headline related to a ticker.
type StockNews struct {
	Title     string `json:"title"`
	Link      string `json:"link"`
	Publisher string `json:"publisher"`
	Time      int64  `json:"time"` // unix seconds
}

// FetchStockNews returns recent news *specific to* a symbol via Yahoo Finance's
// per-symbol headline RSS feed (no key). This is genuinely company-specific,
// unlike the generic search endpoint. Cached for 10 minutes per symbol.
func FetchStockNews(symbol string) ([]StockNews, error) {
	key := "stocknews:" + symbol
	v, err := cached(key, 10*time.Minute, func() (any, error) {
		feedURL := "https://feeds.finance.yahoo.com/rss/2.0/headline?region=US&lang=en-US&s=" + url.QueryEscape(symbol)
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()

		fp := gofeed.NewParser()
		fp.UserAgent = UserAgent
		feed, err := fp.ParseURLWithContext(feedURL, ctx)
		if err != nil {
			return nil, err
		}
		out := make([]StockNews, 0, len(feed.Items))
		for _, it := range feed.Items {
			var ts int64
			if it.PublishedParsed != nil {
				ts = it.PublishedParsed.Unix()
			}
			// The RSS item rarely carries a distinct publisher; fall back to the
			// feed source label. (Author may be nil.)
			pub := "Yahoo Finance"
			if it.Author != nil && it.Author.Name != "" {
				pub = it.Author.Name
			}
			out = append(out, StockNews{Title: it.Title, Link: it.Link, Publisher: pub, Time: ts})
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]StockNews), nil
}
