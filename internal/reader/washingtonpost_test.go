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

func postFixture() map[string]any {
	return map[string]any{"canonical_url": "/business/2026/09/10/test/", "type": "story", "headlines": map[string]string{"basic": "Example title"}, "content_elements": []postElement{
		{Type: "text", Content: paragraph + `<em>Emphasis</em><script>malicious()</script>`},
		{Type: "image", URL: "https://images.example.com/photo.jpg", Caption: "Photo caption"},
		{Type: "text", Content: paragraph}, {Type: "text", Content: paragraph},
	}}
}

func TestWashingtonPostData(t *testing.T) {
	source, _ := url.Parse("https://www.washingtonpost.com/business/2026/09/10/test/?tracking=1")
	cases := []struct {
		name   string
		change func(map[string]any)
		want   bool
	}{
		{"article", func(d map[string]any) {}, true},
		{"other article", func(d map[string]any) { d["canonical_url"] = "/other/" }, false},
		{"other host", func(d map[string]any) { d["canonical_url"] = "https://example.com" + source.Path }, false},
		{"restricted", func(d map[string]any) { d["restricted"] = true }, false},
		{"preview", func(d map[string]any) {
			d["content_elements"] = append(d["content_elements"].([]postElement), postElement{Type: "subscribe-cta"})
		}, false},
		{"short body", func(d map[string]any) { d["content_elements"] = []postElement{{Type: "text", Content: "Preview only"}} }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := postFixture()
			tc.change(data)
			body, _ := json.Marshal(data)
			a, err := extractWashingtonPost(body, source, `<meta property="og:site_name" content="The Washington Post">`)
			if (err == nil) != tc.want {
				t.Fatalf("success=%v want=%v: %v", err == nil, tc.want, err)
			}
			if tc.want {
				if len(a.Paragraphs) != 3 || a.Publisher != "The Washington Post" || !containsImage(a.Content, "https://images.example.com/photo.jpg") {
					t.Fatalf("incomplete extraction: %+v", a)
				}
				encoded, _ := json.Marshal(a.Content)
				if strings.Contains(string(encoded), "malicious") || !strings.Contains(string(encoded), `"tag":"em"`) {
					t.Fatal("sanitization lost formatting or retained scripts")
				}
			}
		})
	}
}

func TestWashingtonPostFetchFallback(t *testing.T) {
	old := client
	defer func() { client = old }()
	for _, tc := range []struct {
		name, host, page string
		status           int
		calls            int
		want             bool
	}{
		{"teaser", "www.washingtonpost.com", "<article>Preview</article>", 200, 2, true},
		{"full HTML", "www.washingtonpost.com", fullArticle(), 200, 1, true},
		{"lookalike host", "www.washingtonpost.com.example.com", "<article>Preview</article>", 200, 1, false},
		{"API unavailable", "www.washingtonpost.com", "<article>Preview</article>", 429, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				status := 200
				kind := "text/html"
				body := tc.page
				if calls == 2 {
					if r.URL.Host != "www.washingtonpost.com" || r.URL.Path != "/arc/prism/api/prism-content-api/" {
						t.Fatalf("unexpected endpoint: %s", r.URL)
					}
					var q map[string]string
					if json.Unmarshal([]byte(r.URL.Query().Get("query")), &q) != nil || q["canonical_url"] != "/business/2026/09/10/test/" {
						t.Fatal("incorrect article query")
					}
					if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
						t.Fatal("credentials forwarded")
					}
					b, _ := json.Marshal(postFixture())
					body = string(b)
					kind = "application/json"
					status = tc.status
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {kind}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			a, err := Fetch(context.Background(), "https://"+tc.host+"/business/2026/09/10/test/?tracking=1", "local")
			if calls != tc.calls || (err == nil) != tc.want {
				t.Fatalf("calls=%d success=%v err=%v", calls, err == nil, err)
			}
			if tc.want && (a.Service != "local" || !strings.Contains(a.SourceURL, "/business/")) {
				t.Fatal("lost original service/source")
			}
		})
	}
}
