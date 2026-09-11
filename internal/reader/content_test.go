package reader

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestRichArticle(t *testing.T) {
	base, _ := url.Parse("https://publisher.example/news/story")
	html := `<html><head><meta property="og:site_name" content="Publisher"><meta name="author" content="A Writer"><meta property="article:published_time" content="2026-09-10T12:00:00Z"><meta property="og:image" content="/cover.jpg"><meta property="og:description" content="The introduction"></head><article><h1>Headline</h1><div class="article__body"><p>` + paragraph + `<strong>Emphasis</strong> and <a href="../source">source</a></p><h2>A section</h2><p>` + paragraph + `</p><blockquote><p>` + paragraph + `</p></blockquote><ol><li>First</li><li>Second</li></ol><figure><img src="/photo.jpg" alt="The scene" onerror="alert(1)"><figcaption>Photo credit</figcaption></figure><pre><code>line one
  line two</code></pre><script>alert('injected')</script><img src="javascript:alert(1)"><a href="javascript:alert(1)">unsafe link</a></div><p>Recommended unrelated content</p></article></html>`
	a, err := extractPage(html, false, base)
	if err != nil {
		t.Fatal(err)
	}
	if a.Title != "Headline" || a.Author != "A Writer" || a.Publisher != "Publisher" || a.Image != "https://publisher.example/cover.jpg" || a.Description != "The introduction" {
		t.Fatalf("missing metadata: %+v", a)
	}
	data, _ := json.Marshal(a.Content)
	text := string(data)
	for _, want := range []string{`"tag":"h2"`, `"tag":"strong"`, `"tag":"blockquote"`, `"tag":"ol"`, `"tag":"figcaption"`, `"tag":"pre"`, `https://publisher.example/source`, `https://publisher.example/photo.jpg`, `Photo credit`, `line one\n  line two`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s in %s", want, text)
		}
	}
	for _, bad := range []string{"javascript:", "onerror", "injected", "Recommended unrelated content"} {
		if strings.Contains(text, bad) {
			t.Errorf("unsafe or unrelated content: %s", bad)
		}
	}
}
func TestResourceURLs(t *testing.T) {
	base, _ := url.Parse("https://publisher.example/news/story")
	for _, bad := range []string{"javascript:alert(1)", "data:image/svg+xml,x", "file:///etc/passwd", "http://127.0.0.1/a", "http://localhost/a", "http://router.local/a", "https://user:pass@publisher.example/a"} {
		if resourceURL(bad, base) != "" {
			t.Errorf("accepted %s", bad)
		}
	}
	if resourceURL("/image.jpg", base) != "https://publisher.example/image.jpg" {
		t.Fatal("relative image not resolved")
	}
}

func TestSplitBodySections(t *testing.T) {
	html := "<article><h1>Split article</h1>"
	for _, marker := range []string{"First section", "Last section"} {
		html += `<div class="article__body"><div class="entry-content"><p>` + marker + paragraph + `</p><p>` + paragraph + `</p><p>` + paragraph + `</p></div></div>`
	}
	html += "</article>"
	a, err := extract(html, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Paragraphs) != 6 {
		t.Fatalf("lost or duplicated body chunks: %d", len(a.Paragraphs))
	}
	if !strings.Contains(a.Paragraphs[0], "First section") || !strings.Contains(a.Paragraphs[3], "Last section") {
		t.Fatal("body order changed")
	}
}
