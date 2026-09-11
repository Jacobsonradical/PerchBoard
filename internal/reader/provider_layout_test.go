package reader

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPublisherBodyContainers(t *testing.T) {
	for _, wrapper := range []string{`<div id="storytext">`, `<div class="ct-rich-text-children">`, `<div class="paywalled_content">`, `<div class="article-body">`, `<section class="content__body">`, `<div class="article-body-viewer-selector">`, `<div data-testid="prism-article-body">`, `<div class="wysiwyg--all-content">`} {
		closing := "</div>"
		if strings.HasPrefix(wrapper, "<section") {
			closing = "</section>"
		}
		body := `<meta property="og:type" content="article"><main><p>Biography outside the article.</p>` + wrapper + "<p>" + paragraph + "</p><p>" + paragraph + "</p>" + closing + `<aside>Advertising</aside>` + wrapper + "<p>" + paragraph + "</p><p>" + paragraph + "</p>" + closing + "</main>"
		a, e := extract(body, false)
		if e != nil || len(a.Paragraphs) != 4 {
			t.Fatalf("%s: paragraphs=%d err=%v", wrapper, len(a.Paragraphs), e)
		}
		if strings.Contains(strings.Join(a.Paragraphs, " "), "Biography") {
			t.Fatal("included outside material")
		}
	}
}

func TestCNNBoundedHTMLAllowance(t *testing.T) {
	old := client
	defer func() { client = old }()
	for _, tc := range []struct {
		host string
		size int
		want bool
	}{{"www.cnn.com", 5 << 20, true}, {"example.com", 5 << 20, false}, {"www.cnn.com.example.com", 5 << 20, false}, {"www.cnn.com", (8 << 20) + 1, false}} {
		body := fullArticle() + "<!--" + strings.Repeat("x", tc.size) + "-->"
		client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})}
		_, e := Fetch(context.Background(), "https://"+tc.host+"/story", "local")
		if (e == nil) != tc.want {
			t.Fatalf("host=%s size=%d success=%v err=%v", tc.host, tc.size, e == nil, e)
		}
	}
}

func TestFullStructuredBodyOverPreview(t *testing.T) {
	source, _ := url.Parse("https://www.scmp.com/news/article/123/example")
	for _, matches := range []bool{true, false} {
		entity := source.String()
		if !matches {
			entity = "https://www.scmp.com/news/article/456/other"
		}
		data, _ := json.Marshal(map[string]any{"@type": "NewsArticle", "mainEntityOfPage": entity, "articleBody": strings.Repeat(paragraph+"\n", 8)})
		page := fullArticle() + `<script type="application/ld+json">` + string(data) + `</script>`
		a, err := extractPage(page, false, source)
		want := 3
		if matches {
			want = 8
		}
		if err != nil || len(a.Paragraphs) != want {
			t.Fatalf("matched=%v paragraphs=%d err=%v", matches, len(a.Paragraphs), err)
		}
	}
}

func TestRejectCollectionsAndPartialLiveUpdates(t *testing.T) {
	collection := `<meta property="og:type" content="website"><article><h2>Story one</h2><p class="promo-description">` + paragraph + `</p><h2>Story two</h2><p class="promo-description">` + paragraph + `</p><h2>Story three</h2><p class="promo-description">` + paragraph + `</p></article>`
	if _, err := extract(collection, false); err == nil {
		t.Fatal("accepted collection summaries")
	}
	data, _ := json.Marshal(map[string]any{"@type": "LiveBlogPosting", "liveBlogUpdate": []any{map[string]any{"@type": "BlogPosting", "articleBody": paragraph + paragraph}}})
	if _, err := extract(`<script type="application/ld+json">`+string(data)+`</script>`+fullArticle(), false); err == nil {
		t.Fatal("accepted partial live coverage")
	}
}
