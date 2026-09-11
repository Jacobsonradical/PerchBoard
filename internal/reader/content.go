package reader

import (
	"net"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Content contains only an explicit set of document elements and attributes.
// The browser renders this tree itself; no publisher HTML or CSS is injected.
type Content struct {
	Tag      string    `json:"tag,omitempty"`
	Text     string    `json:"text,omitempty"`
	URL      string    `json:"url,omitempty"`
	Alt      string    `json:"alt,omitempty"`
	Children []Content `json:"children,omitempty"`
}

func resourceURL(raw string, base *url.URL) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || raw == "" {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if _, err = ValidateURL(u.String()); err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		if !publicIP(ip) {
			return ""
		}
	} else if !strings.Contains(host, ".") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return ""
	}
	return u.String()
}

func contentNodes(parent *html.Node, base *url.URL) []Content {
	var out []Content
	for n := parent.FirstChild; n != nil; n = n.NextSibling {
		if n.Type == html.TextNode {
			out = append(out, Content{Text: n.Data})
			continue
		}
		if n.Type != html.ElementNode {
			continue
		}
		attrs := map[string]string{}
		for _, a := range n.Attr {
			attrs[a.Key] = a.Val
		}
		if _, hidden := attrs["hidden"]; hidden || attrs["aria-hidden"] == "true" {
			continue
		}
		tag := n.Data
		switch tag {
		case "script", "style", "noscript", "iframe", "object", "embed", "svg", "form", "input", "button", "nav", "aside", "footer", "video", "audio":
			continue
		case "img":
			src := resourceURL(attrs["src"], base)
			if src == "" {
				src = resourceURL(attrs["data-src"], base)
			}
			if src != "" && attrs["width"] != "1" && attrs["height"] != "1" {
				out = append(out, Content{Tag: "img", URL: src, Alt: attrs["alt"]})
			}
			continue
		}
		children := contentNodes(n, base)
		switch tag {
		case "p", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "blockquote", "pre", "code", "strong", "em", "b", "i", "s", "sup", "sub", "figure", "figcaption", "table", "thead", "tbody", "tr", "th", "td", "br", "hr":
			out = append(out, Content{Tag: tag, Children: children})
		case "a":
			href := resourceURL(attrs["href"], base)
			if href == "" {
				out = append(out, children...)
			} else {
				out = append(out, Content{Tag: "a", URL: href, Children: children})
			}
		case "h1": // The title is rendered in the article header.
		default:
			out = append(out, children...)
		}
	}
	return out
}

func metadata(doc *goquery.Document, base *url.URL) Article {
	meta := func(selector string) string { v, _ := doc.Find(selector).First().Attr("content"); return clean(v) }
	a := Article{
		Publisher:   meta(`meta[property="og:site_name"]`),
		Description: meta(`meta[property="og:description"]`),
		Author:      meta(`meta[property="article:author"]`),
		Published:   meta(`meta[property="article:published_time"]`),
		Image:       resourceURL(meta(`meta[property="og:image"]`), base),
	}
	if a.Author == "" {
		a.Author = meta(`meta[name="author"]`)
	}
	if a.Published == "" {
		a.Published, _ = doc.Find("article time[datetime], time[datetime]").First().Attr("datetime")
	}
	if a.Publisher == "" && base != nil {
		a.Publisher = base.Hostname()
	}
	return a
}

func containsImage(nodes []Content, src string) bool {
	for _, n := range nodes {
		if n.Tag == "img" && n.URL == src {
			return true
		}
		if containsImage(n.Children, src) {
			return true
		}
	}
	return false
}
