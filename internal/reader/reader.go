// Package reader extracts article text from publisher pages or existing archives.
package reader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
)

const maxBody = 4 << 20

type Article struct {
	Title       string    `json:"title"`
	Publisher   string    `json:"publisher,omitempty"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	Published   string    `json:"published,omitempty"`
	Image       string    `json:"image,omitempty"`
	Content     []Content `json:"content,omitempty"`
	Paragraphs  []string  `json:"paragraphs"`
	SourceURL   string    `json:"sourceUrl"`
	Service     string    `json:"service"`
}

// ValidateURL rejects credentials and non-web schemes before any network access.
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("enter a public HTTP or HTTPS article URL")
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		return nil, errors.New("article URLs must use a standard web port")
	}
	return u, nil
}

func publicIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "2002::/16"} {
		if netip.MustParsePrefix(cidr).Contains(a) {
			return false
		}
	}
	return true
}

// Resolve and dial the checked IP itself, including on redirects, to prevent
// DNS rebinding from turning an article fetch into a request to local services.
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, errors.New("could not resolve the article host")
	}
	if len(ips) == 0 {
		return nil, errors.New("article host has no address")
	}
	for _, ip := range ips {
		if !publicIP(ip.IP) {
			return nil, errors.New("only public article hosts are supported")
		}
	}
	var last error
	for _, ip := range ips {
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

var client = &http.Client{
	Timeout: 20 * time.Second,
	// A custom dialer disables automatic HTTP/2 unless explicitly enabled.
	// Keep protocol negotiation while dialing only validated public addresses.
	Transport: &http.Transport{DialContext: publicDial, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 12 * time.Second, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		_, err := ValidateURL(req.URL.String())
		return err
	},
}

func Fetch(ctx context.Context, raw, service string) (Article, error) {
	u, err := ValidateURL(raw)
	if err != nil {
		return Article{}, err
	}
	u.Fragment = ""
	target := u.String()
	if service == "archive" {
		target = "https://archive.ph/newest/" + target
	} else if service != "local" {
		return Article{}, errors.New("unknown reading service")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Article{}, errors.New("invalid article URL")
	}
	req.Header.Set("User-Agent", "PerchBoard/0.1 article reader")
	req.Header.Set("Accept", "text/html, application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		return Article{}, errors.New("service could not be reached or the article host is unsupported")
	}
	defer resp.Body.Close()
	if service == "local" && strings.Trim(u.Path, "/") != "" && strings.Trim(resp.Request.URL.Path, "/") == "" {
		return Article{}, errors.New("the article redirected to the publisher's homepage")
	}
	if resp.StatusCode != http.StatusOK {
		if service == "local" && newYorkTimesURL(resp.Request.URL) && resp.StatusCode == http.StatusForbidden {
			return Article{}, errors.New("New York Times blocked this request (HTTP 403); no article content was delivered")
		}
		return Article{}, fmt.Errorf("service returned HTTP %d", resp.StatusCode)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml+xml") {
		return Article{}, errors.New("service did not return an HTML article")
	}
	limit := maxBody
	// CNN's current article HTML includes over 4 MiB of page state. Keep a
	// bounded, publisher-specific allowance instead of accepting truncated HTML.
	if host := strings.ToLower(resp.Request.URL.Hostname()); host == "www.cnn.com" || host == "edition.cnn.com" || host == "cnn.com" {
		limit = 8 << 20
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return Article{}, errors.New("could not download the article")
	}
	if len(body) > limit {
		return Article{}, errors.New("article page exceeds the reader's size limit")
	}
	article, err := extractPage(string(body), service == "archive", resp.Request.URL)
	if err != nil && service == "local" && washingtonPostURL(resp.Request.URL) {
		article, err = fetchWashingtonPost(ctx, resp.Request.URL, string(body))
	}
	if err != nil {
		return Article{}, err
	}
	article.SourceURL = resp.Request.URL.String()
	article.Service = service
	return article, nil
}

func clean(s string) string  { return strings.Join(strings.Fields(s), " ") }
func length(ps []string) int { return utf8.RuneCountInString(strings.Join(ps, " ")) }

func extract(html string, archive bool) (Article, error) {
	return extractPage(html, archive, nil)
}

func extractPage(html string, archive bool, base *url.URL) (Article, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return Article{}, errors.New("could not parse the article")
	}
	if !archive && newYorkTimesURL(base) {
		var state string
		doc.Find("script:not([src])").Each(func(_ int, s *goquery.Selection) {
			if strings.HasPrefix(strings.TrimSpace(s.Text()), "window.__preloadedData =") {
				state = strings.TrimSpace(s.Text())[len("window.__preloadedData ="):]
			}
		})
		if state != "" {
			return extractNewYorkTimes(state, doc, base)
		}
	}
	article := metadata(doc, base)
	root := doc.Selection
	if archive {
		root = doc.Find("#CONTENT")
		if root.Length() != 1 {
			return Article{}, errors.New("Archive.today did not return a readable snapshot (it may require verification or have no saved copy)")
		}
	}
	title := clean(root.Find("h1").First().Text())
	if title == "" {
		title = clean(doc.Find("title").First().Text())
	}
	var structured []string
	structuredMatchesSource := false
	liveCoverage := false
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, child := range x {
				walk(child)
			}
		case map[string]any:
			if x["@type"] == "LiveBlogPosting" {
				liveCoverage = true
				return
			}
			if body, ok := x["articleBody"].(string); ok {
				// Decode any markup carried inside JSON-LD without rendering it.
				d, e := goquery.NewDocumentFromReader(strings.NewReader(body))
				if e == nil {
					d.Find("script,style,iframe,form").Remove()
					var ps []string
					for _, line := range strings.Split(d.Text(), "\n") {
						if text := clean(line); text != "" {
							ps = append(ps, text)
						}
					}
					if length(ps) > length(structured) {
						structured = ps
						structuredMatchesSource = false
						entity, _ := x["mainEntityOfPage"].(string)
						if obj, ok := x["mainEntityOfPage"].(map[string]any); ok {
							entity, _ = obj["@id"].(string)
						}
						if base != nil && entity != "" {
							if u, err := url.Parse(entity); err == nil {
								u = base.ResolveReference(u)
								structuredMatchesSource = strings.EqualFold(u.Hostname(), base.Hostname()) && strings.TrimRight(u.Path, "/") == strings.TrimRight(base.Path, "/")
							}
						}
					}
				}
			}
			for _, child := range x {
				walk(child)
			}
		}
	}
	root.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		var v any
		if json.Unmarshal([]byte(s.Text()), &v) == nil {
			walk(v)
		}
	})
	if liveCoverage {
		return Article{}, errors.New("live coverage is not supported by this article reader; open the original page for all updates")
	}
	root.Find("script, style, noscript, nav, aside, footer, form, button, iframe, .byline-bio, [hidden], [aria-hidden=true]").Remove()
	var best []string
	var selected *goquery.Selection
	bestPriority := 0
	const bodySelectors = "[itemprop=articleBody], .article__body, .entry-content, .post-content, [data-testid=BodyWrapper], #storytext, .article-body, .content__body, .article-body-viewer-selector, [data-testid=prism-article-body], .wysiwyg--all-content, .ct-rich-text-children, .paywalled_content, #detailContent .story, .content-area > .rich-text"
	candidates := root.Find(bodySelectors + ", article, main, [role=main]")
	// Publishers can split the body into multiple wrappers around ad slots.
	// Keep all those sections in document order instead of selecting one chunk.
	root.Find("article, main, [role=main]").Each(func(_ int, s *goquery.Selection) {
		if s.Find("article").Length() > 1 {
			return
		}
		parts := s.Find(bodySelectors).FilterFunction(func(_ int, part *goquery.Selection) bool {
			return part.ParentsFiltered(bodySelectors).Length() == 0
		})
		if parts.Length() < 2 {
			return
		}
		joined, _ := goquery.NewDocumentFromReader(strings.NewReader(`<div itemprop="articleBody"></div>`))
		container := joined.Find("div")
		parts.Each(func(_ int, part *goquery.Selection) { container.AppendSelection(part.Clone()) })
		candidates = candidates.AddSelection(container)
	})
	candidates.Each(func(_ int, s *goquery.Selection) {
		if s.Find(".promo-description").Length() > 1 && s.Find("p:not(.promo-description)").Length() == 0 {
			return
		}
		// A homepage or topic index may contain many article cards. Do not
		// combine their summaries into a purported article body.
		if s.Find("article").Length() > 1 {
			return
		}
		if s.Is("main, [role=main]") {
			kind, _ := doc.Find(`meta[property="og:type"]`).Attr("content")
			if kind != "article" {
				return
			}
		}
		var ps []string
		bodyParagraphs := 0
		s.Find("p, h2, h3, blockquote").Each(func(_ int, p *goquery.Selection) {
			if p.ParentsFiltered("blockquote").Length() > 0 {
				return
			}
			if txt := clean(p.Text()); txt != "" {
				ps = append(ps, txt)
				if p.Is("p, blockquote") && utf8.RuneCountInString(txt) >= 80 {
					bodyParagraphs++
				}
			}
		})
		priority := 1
		if s.Is(bodySelectors) {
			priority = 2
		}
		if bodyParagraphs >= 3 && length(ps) >= 600 && (priority > bestPriority || (priority == bestPriority && length(ps) > length(best))) {
			best = ps
			selected = s
			bestPriority = priority
		}
	})
	if (length(best) < 600 && length(structured) > length(best)) || (structuredMatchesSource && length(structured) > length(best)*3/2) {
		best = structured
		selected = nil
	}
	// A substantial body is evidence of extractable text, not proof of completeness.
	// Reject recognizable subscription prompts instead of presenting a teaser as success.
	text := strings.ToLower(strings.Join(best, " "))
	blocked := false
	for _, phrase := range []string{"subscribe to continue reading", "subscribe to read the full", "sign in to continue reading", "unlock this article", "already a subscriber?", "enable javascript and cookies to continue", "verify you are human"} {
		if strings.Contains(text, phrase) {
			blocked = true
		}
	}
	if length(best) < 600 || blocked {
		return Article{}, errors.New("no substantial article body found; the page may be a preview, paywall, verification page, or unsupported layout")
	}
	article.Title = title
	article.Paragraphs = best
	if selected != nil {
		article.Content = contentNodes(selected.Get(0), base)
		if containsImage(article.Content, article.Image) {
			article.Image = ""
		}
	}
	return article, nil
}
