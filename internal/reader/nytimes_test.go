package reader

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func nytFixture(source string, count int) string {
	blocks := []any{}
	for i := 0; i < count; i++ {
		blocks = append(blocks, map[string]any{"__typename": "ParagraphBlock", "content": []any{map[string]string{"__typename": "TextInline", "text": paragraph + "undefined <script>text only</script>"}}})
	}
	b, _ := json.Marshal(map[string]any{"loaderData": map[string]any{"data": map[string]any{"article": map[string]any{"url": source, "headline": map[string]string{"default": "Example headline"}, "sprinkledBody": map[string]any{"content": blocks}}}}})
	return `<script>window.__preloadedData = ` + strings.TrimSuffix(string(b), "}") + `,"optional":undefined};</script>`
}
func TestNewYorkTimesEmbeddedBody(t *testing.T) {
	source, _ := url.Parse("https://www.nytimes.com/2026/09/11/example.html")
	for _, tc := range []struct {
		name, url string
		count     int
		want      bool
	}{
		{"body", source.String(), 3, true},
		{"wrong article", "https://www.nytimes.com/other.html", 3, false},
		{"wrong host", "https://www.nytimes.com.example.com/2026/09/11/example.html", 3, false},
		{"teaser", source.String(), 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, e := extractPage(nytFixture(tc.url, tc.count), false, source)
			if (e == nil) != tc.want {
				t.Fatalf("success=%v err=%v", e == nil, e)
			}
			if tc.want {
				if len(a.Paragraphs) != 3 || !strings.Contains(a.Paragraphs[0], "undefined <script>text only</script>") {
					t.Fatal("body text changed")
				}
				b, _ := json.Marshal(a.Content)
				if strings.Contains(string(b), `"tag":"script"`) {
					t.Fatal("executable content")
				}
			}
		})
	}
	// Embedded data takes precedence over a misleading, substantial DOM preview.
	_, e := extractPage(fullArticle()+nytFixture(source.String(), 1), false, source)
	if e == nil {
		t.Fatal("accepted DOM preview after incomplete embedded data")
	}
}

func TestNewYorkTimesAdditionalBlocks(t *testing.T) {
	source, _ := url.Parse("https://www.nytimes.com/2026/09/11/example.html")
	original := nytFixture(source.String(), 3)
	detail := strings.Replace(original, `"__typename":"ParagraphBlock"`, `"__typename":"DetailBlock"`, 1)
	a, err := extractPage(detail, false, source)
	if err != nil || len(a.Paragraphs) != 3 {
		t.Fatalf("detail block: %v", err)
	}
	// Append an interactive block while preserving three substantial paragraphs.
	token := `"sprinkledBody":{"content":[`
	interactive := strings.Replace(original, token, token+`{"__typename":"InteractiveBlock"},`, 1)
	a, err = extractPage(interactive, false, source)
	if err != nil || !strings.Contains(strings.Join(a.Paragraphs, " "), "View interactive content in the original article.") {
		t.Fatalf("interactive notice: %v", err)
	}
	encoded, _ := json.Marshal(a.Content)
	if !strings.Contains(string(encoded), source.String()) {
		t.Fatal("missing original link")
	}
	unknown := strings.Replace(original, `"__typename":"ParagraphBlock"`, `"__typename":"UnknownBlock"`, 1)
	if _, err = extractPage(unknown, false, source); err == nil {
		t.Fatal("silently dropped unknown content")
	}
	video := `<script>window.__preloadedData = {"initialState":{"video":{"__typename":"Video","url":"` + source.String() + `"}}};</script>`
	if _, err = extractPage(video, false, source); err == nil || !strings.Contains(err.Error(), "video") {
		t.Fatalf("video diagnosis: %v", err)
	}
}

func TestNewYorkTimesRelatedStories(t *testing.T) {
	source, _ := url.Parse("https://www.nytimes.com/2026/09/11/example.html")
	original := nytFixture(source.String(), 3)
	token := `"sprinkledBody":{"content":[`
	page := strings.Replace(original, token, token+`{"__typename":"RelatedLinksBlock","related":[{"url":"https://example.com/other","summary":"Unrelated recommendation"}]},`, 1)
	article, err := extractPage(page, false, source)
	if err != nil || len(article.Paragraphs) != 3 {
		t.Fatalf("lost article body: %v", err)
	}
	if strings.Contains(strings.Join(article.Paragraphs, " "), "Unrelated recommendation") {
		t.Fatal("mixed recommended stories into article")
	}
}

func TestNewYorkTimesSectionHeadings(t *testing.T) {
	source, _ := url.Parse("https://www.nytimes.com/2026/09/11/example.html")
	token := `"sprinkledBody":{"content":[`
	page := strings.Replace(nytFixture(source.String(), 3), token, token+`{"__typename":"Heading2Block","content":[{"__typename":"TextInline","text":"Section heading"}]},`, 1)
	a, err := extractPage(page, false, source)
	if err != nil {
		t.Fatal(err)
	}
	headings := 0
	for _, node := range a.Content {
		if node.Tag == "h2" {
			headings++
		}
	}
	if headings != 1 || len(a.Paragraphs) != 4 || a.Paragraphs[0] != "Section heading" {
		t.Fatal("section heading/order lost")
	}
	onlyHeadings := strings.ReplaceAll(nytFixture(source.String(), 3), "ParagraphBlock", "Heading2Block")
	if _, err = extractPage(onlyHeadings, false, source); err == nil {
		t.Fatal("accepted headings as substantial body")
	}
}
