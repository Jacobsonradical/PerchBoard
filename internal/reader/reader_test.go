package reader

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestPublicDestinations(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "https://user:password@example.com", "https://example.com:8080"} {
		if _, err := ValidateURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "224.0.0.1", "fc00::1", "2001:db8::1", "64:ff9b::7f00:1"} {
		if publicIP(net.ParseIP(raw)) {
			t.Errorf("accepted nonpublic IP %s", raw)
		}
	}
	if !publicIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("rejected public IP")
	}
	if _, err := publicDial(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("dial accepted localhost")
	}
}

var paragraph = strings.Repeat("A publisher supplied this sentence as part of the article body. ", 6)

func fullArticle() string {
	return "<html><title>Test article</title><article><h1>Test article</h1><p>" + paragraph + "</p><p>" + paragraph + "</p><p>" + paragraph + "</p></article></html>"
}

func TestExtract(t *testing.T) {
	cases := []struct {
		name, html    string
		archive, want bool
	}{
		{"article", fullArticle(), false, true},
		{"archive", "<div id='CONTENT'>" + fullArticle() + "</div>", true, true},
		{"archive search page", fullArticle(), true, false},
		{"topic index", "<main><article><p>" + paragraph + "</p></article><article><p>" + paragraph + "</p></article><article><p>" + paragraph + "</p></article></main>", false, false},
		{"teaser", "<article><p>Only the first sentence is available.</p></article>", false, false},
		{"long paywall", strings.Replace(fullArticle(), "</article>", "<p>Subscribe to continue reading</p></article>", 1), false, false},
		{"challenge", "<title>Verify you are human</title><p>Checking your browser</p>", false, false},
		{"jsonld", fmt.Sprintf(`<script type="application/ld+json">{"@graph":[{"@type":"NewsArticle","articleBody":%q}]}</script>`, paragraph+paragraph), false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, err := extract(c.html, c.archive)
			if (err == nil) != c.want {
				t.Fatalf("success=%v want=%v: %v", err == nil, c.want, err)
			}
			if c.want && len(a.Paragraphs) == 0 {
				t.Fatal("empty article")
			}
		})
	}
	a, err := extract(strings.Replace(fullArticle(), "</article>", "<script>alert('secret')</script><form><p>private form</p></form></article>", 1), false)
	if err != nil || strings.Contains(strings.Join(a.Paragraphs, " "), "secret") || strings.Contains(strings.Join(a.Paragraphs, " "), "private form") {
		t.Fatal("unsafe content leaked")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestFetch(t *testing.T) {
	old := client
	defer func() { client = old }()
	client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Path = "/"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(fullArticle())), Request: r}, nil
	})}
	if _, err := Fetch(context.Background(), "https://example.com/missing-story", "local"); err == nil {
		t.Fatal("accepted redirect to homepage")
	}
	for _, service := range []string{"local", "archive"} {
		t.Run(service, func(t *testing.T) {
			client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if service == "archive" && !strings.HasPrefix(r.URL.String(), "https://archive.ph/newest/https://example.com/article?a=1&b=2") {
					t.Fatal(r.URL)
				}
				body := fullArticle()
				if service == "archive" {
					body = "<div id='CONTENT'>" + body + "</div>"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			a, err := Fetch(context.Background(), "https://example.com/article?a=1&b=2#part", service)
			if err != nil || a.Service != service {
				t.Fatalf("%+v %v", a, err)
			}
		})
	}
	for _, status := range []int{403, 429, 500} {
		client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("blocked")), Request: r}, nil
		})}
		if _, err := Fetch(context.Background(), "https://example.com/article", "archive"); err == nil {
			t.Fatalf("accepted HTTP %d", status)
		}
	}
	client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxBody+1))), Request: r}, nil
	})}
	if _, err := Fetch(context.Background(), "https://example.com/article", "local"); err == nil {
		t.Fatal("accepted oversized body")
	}
}
