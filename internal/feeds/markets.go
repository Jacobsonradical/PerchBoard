package feeds

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Quote is a normalized market quote with a short intraday sparkline and the
// key stats shown in the detail view.
type Quote struct {
	Symbol        string    `json:"symbol"`
	Name          string    `json:"name"` // company / instrument name
	Currency      string    `json:"currency"`
	Price         float64   `json:"price"`
	PreviousClose float64   `json:"previousClose"`
	Change        float64   `json:"change"`        // absolute
	ChangePercent float64   `json:"changePercent"` // %
	DayHigh       float64   `json:"dayHigh"`
	DayLow        float64   `json:"dayLow"`
	Week52High    float64   `json:"week52High"`
	Week52Low     float64   `json:"week52Low"`
	Volume        int64     `json:"volume"`
	Spark         []float64 `json:"spark"`      // intraday closes for the mini chart
	SparkTimes    []int64   `json:"sparkTimes"` // unix seconds, parallel to Spark
	Error         string    `json:"error,omitempty"`
}

// SymbolResult is one hit from a company/ticker search.
type SymbolResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Exchange string `json:"exchange"`
	Type     string `json:"type"`
}

// FetchMarkets returns quotes for the given symbols (e.g. AAPL, BTC-USD, ^GSPC).
// Each is fetched concurrently from Yahoo Finance's public chart API (no key).
// Cached for 2 minutes per symbol set.
func FetchMarkets(symbols []string) ([]Quote, error) {
	// Normalize + cap.
	clean := make([]string, 0, len(symbols))
	for _, s := range symbols {
		s = strings.TrimSpace(strings.ToUpper(s))
		if s != "" {
			clean = append(clean, s)
		}
	}
	if len(clean) == 0 {
		return []Quote{}, nil
	}
	key := "markets:" + strings.Join(clean, ",")

	v, err := cached(key, 2*time.Minute, func() (any, error) {
		quotes := make([]Quote, len(clean))
		var wg sync.WaitGroup
		for i, sym := range clean {
			wg.Add(1)
			go func(i int, sym string) {
				defer wg.Done()
				quotes[i] = fetchQuote(sym)
			}(i, sym)
		}
		wg.Wait()
		return quotes, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]Quote), nil
}

// fetchQuote pulls a single symbol's intraday chart. Errors are attached to the
// quote (not fatal) so one bad ticker doesn't blank the whole widget.
func fetchQuote(sym string) Quote {
	q := Quote{Symbol: sym}
	api := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=1d&interval=15m", sym)
	var raw struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Currency      string  `json:"currency"`
					ShortName     string  `json:"shortName"`
					LongName      string  `json:"longName"`
					Price         float64 `json:"regularMarketPrice"`
					PreviousClose float64 `json:"chartPreviousClose"`
					DayHigh       float64 `json:"regularMarketDayHigh"`
					DayLow        float64 `json:"regularMarketDayLow"`
					Volume        int64   `json:"regularMarketVolume"`
					Week52High    float64 `json:"fiftyTwoWeekHigh"`
					Week52Low     float64 `json:"fiftyTwoWeekLow"`
				} `json:"meta"`
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
			Error any `json:"error"`
		} `json:"chart"`
	}
	if err := getJSON(api, &raw); err != nil {
		q.Error = err.Error()
		return q
	}
	if len(raw.Chart.Result) == 0 {
		q.Error = "no data"
		return q
	}
	r := raw.Chart.Result[0]
	q.Currency = r.Meta.Currency
	q.Name = r.Meta.ShortName
	if q.Name == "" {
		q.Name = r.Meta.LongName
	}
	q.Price = r.Meta.Price
	q.PreviousClose = r.Meta.PreviousClose
	q.DayHigh = r.Meta.DayHigh
	q.DayLow = r.Meta.DayLow
	q.Volume = r.Meta.Volume
	q.Week52High = r.Meta.Week52High
	q.Week52Low = r.Meta.Week52Low
	if q.PreviousClose != 0 {
		q.Change = q.Price - q.PreviousClose
		q.ChangePercent = q.Change / q.PreviousClose * 100
	}
	// Build the sparkline from non-null closes, keeping timestamps aligned so the
	// frontend can show price + time on hover.
	if len(r.Indicators.Quote) > 0 {
		closes := r.Indicators.Quote[0].Close
		for i, c := range closes {
			if c != nil {
				q.Spark = append(q.Spark, *c)
				if i < len(r.Timestamp) {
					q.SparkTimes = append(q.SparkTimes, r.Timestamp[i])
				} else {
					q.SparkTimes = append(q.SparkTimes, 0)
				}
			}
		}
	}
	return q
}

// SearchSymbols looks up tickers by company name or symbol (Yahoo search, no
// key). E.g. "nvidia" -> NVDA. Cached for an hour per query.
func SearchSymbols(query string) ([]SymbolResult, error) {
	v, err := cached("symsearch:"+query, time.Hour, func() (any, error) {
		api := "https://query1.finance.yahoo.com/v1/finance/search?newsCount=0&quotesCount=8&q=" + url.QueryEscape(query)
		var raw struct {
			Quotes []struct {
				Symbol    string `json:"symbol"`
				ShortName string `json:"shortname"`
				LongName  string `json:"longname"`
				Exchange  string `json:"exchDisp"`
				Type      string `json:"quoteType"`
			} `json:"quotes"`
		}
		if err := getJSON(api, &raw); err != nil {
			return nil, err
		}
		out := make([]SymbolResult, 0, len(raw.Quotes))
		for _, q := range raw.Quotes {
			name := q.ShortName
			if name == "" {
				name = q.LongName
			}
			if q.Symbol == "" {
				continue
			}
			out = append(out, SymbolResult{Symbol: q.Symbol, Name: name, Exchange: q.Exchange, Type: q.Type})
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]SymbolResult), nil
}
