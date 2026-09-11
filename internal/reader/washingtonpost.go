package reader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func washingtonPostURL(u *url.URL) bool {
	return u != nil && (strings.EqualFold(u.Hostname(), "www.washingtonpost.com") || strings.EqualFold(u.Hostname(), "washingtonpost.com"))
}

type postElement struct {
	Type    string        `json:"type"`
	Content string        `json:"content"`
	URL     string        `json:"url"`
	Caption string        `json:"caption"`
	Alt     string        `json:"alt_text"`
	Items   []postElement `json:"items"`
}

// The publisher's client requests this same endpoint to replace a restricted
// teaser. Use our checked HTTP client, without forwarding browser credentials.
func fetchWashingtonPost(ctx context.Context, source *url.URL, page string) (Article, error) {
	query, _ := json.Marshal(map[string]string{"canonical_url": source.Path})
	target := "https://www.washingtonpost.com/arc/prism/api/prism-content-api/?" + url.Values{
		"_website": {"washpost"}, "query": {string(query)},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Article{}, err
	}
	req.Header.Set("User-Agent", "PerchBoard/0.1 article reader")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Article{}, errors.New("Washington Post's article data could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Article{}, fmt.Errorf("Washington Post's article data returned HTTP %d", resp.StatusCode)
	}
	if !washingtonPostURL(resp.Request.URL) || !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		return Article{}, errors.New("Washington Post did not return article data")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return Article{}, errors.New("could not read Washington Post's article data")
	}
	return extractWashingtonPost(body, source, page)
}

func extractWashingtonPost(body []byte, source *url.URL, page string) (Article, error) {
	var data struct {
		Canonical  string `json:"canonical_url"`
		Type       string `json:"type"`
		Restricted bool   `json:"restricted"`
		Headlines  struct {
			Basic string `json:"basic"`
		} `json:"headlines"`
		Elements []postElement `json:"content_elements"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return Article{}, errors.New("invalid Washington Post article data")
	}
	canonical, err := url.Parse(data.Canonical)
	if err != nil || data.Canonical == "" {
		return Article{}, errors.New("Washington Post article data has no valid source")
	}
	canonical = source.ResolveReference(canonical)
	if !washingtonPostURL(canonical) || strings.TrimRight(canonical.Path, "/") != strings.TrimRight(source.Path, "/") || data.Type != "story" || data.Restricted || data.Headlines.Basic == "" {
		return Article{}, errors.New("Washington Post returned restricted or different article data")
	}
	var out strings.Builder
	out.WriteString("<h1>" + html.EscapeString(data.Headlines.Basic) + "</h1><article>")
	for _, e := range data.Elements {
		switch e.Type {
		case "subscribe-cta":
			return Article{}, errors.New("Washington Post returned a subscription preview")
		case "text":
			out.WriteString("<p>" + e.Content + "</p>")
		case "header":
			out.WriteString("<h2>" + e.Content + "</h2>")
		case "image":
			out.WriteString(`<figure><img src="` + html.EscapeString(e.URL) + `" alt="` + html.EscapeString(e.Alt) + `">`)
			if e.Caption != "" {
				out.WriteString("<figcaption>" + e.Caption + "</figcaption>")
			}
			out.WriteString("</figure>")
		case "list":
			out.WriteString("<ul>")
			for _, item := range e.Items {
				out.WriteString("<li>" + item.Content + "</li>")
			}
			out.WriteString("</ul>")
		}
	}
	out.WriteString("</article>")
	// Reuse the semantic sanitizer and substantial-body checks. Publisher markup
	// is never returned as executable HTML to the reader UI.
	a, err := extractPage(out.String(), false, source)
	if err != nil {
		return Article{}, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(page))
	if err != nil {
		return Article{}, err
	}
	meta := metadata(doc, source)
	meta.Title, meta.Paragraphs, meta.Content = a.Title, a.Paragraphs, a.Content
	if containsImage(meta.Content, meta.Image) {
		meta.Image = ""
	}
	return meta, nil
}
