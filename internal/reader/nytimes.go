package reader

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func newYorkTimesURL(u *url.URL) bool {
	return u != nil && (strings.EqualFold(u.Hostname(), "www.nytimes.com") || strings.EqualFold(u.Hostname(), "nytimes.com"))
}

// Match strings first so literal occurrences of "undefined" remain unchanged.
// The publisher's embedded object uses undefined for some optional fields.
var stateTokens = regexp.MustCompile(`"(?:\\.|[^"\\])*"|\bundefined\b`)

func extractNewYorkTimes(state string, doc *goquery.Document, source *url.URL) (Article, error) {
	state = stateTokens.ReplaceAllStringFunc(state, func(token string) string {
		if token == "undefined" {
			return "null"
		}
		return token
	})
	var data struct {
		InitialState map[string]struct {
			Type string `json:"__typename"`
			URL  string `json:"url"`
		} `json:"initialState"`
		LoaderData struct {
			Data struct {
				Article struct {
					URL      string `json:"url"`
					Headline struct {
						Default string `json:"default"`
					} `json:"headline"`
					SprinkledBody struct {
						Content []struct {
							Type    string `json:"__typename"`
							Content []struct {
								Type string `json:"__typename"`
								Text string `json:"text"`
							} `json:"content"`
						} `json:"content"`
					} `json:"sprinkledBody"`
				} `json:"article"`
			} `json:"data"`
		} `json:"loaderData"`
	}
	// Decode only data. Never execute the assignment or other page JavaScript.
	if err := json.NewDecoder(strings.NewReader(state)).Decode(&data); err != nil {
		return Article{}, errors.New("could not parse New York Times article data")
	}
	story := data.LoaderData.Data.Article
	if story.URL == "" {
		for _, item := range data.InitialState {
			u, err := url.Parse(item.URL)
			if err == nil && newYorkTimesURL(u) && u.Path == source.Path && item.Type == "Video" {
				return Article{}, errors.New("this New York Times page is a video; the text reader cannot play it, so open the original page")
			}
		}
	}
	canonical, err := url.Parse(story.URL)
	if err != nil || !newYorkTimesURL(canonical) || canonical.Path != source.Path || story.Headline.Default == "" {
		return Article{}, errors.New("New York Times returned missing or different article data")
	}
	var body strings.Builder
	body.WriteString("<h1>" + html.EscapeString(story.Headline.Default) + "</h1><article>")
	for _, block := range story.SprinkledBody.Content {
		tag := "p"
		switch block.Type {
		case "HeaderBasicBlock", "ImageBlock", "Dropzone", "RelatedLinksBlock":
			// Related story recommendations are navigation, not this article's body.
			continue
		case "ParagraphBlock", "DetailBlock":
		case "Heading2Block":
			tag = "h2"
		case "InteractiveBlock":
			// Interactive embeds may contain essential charts or other material.
			// Keep their position visible instead of silently dropping the block.
			body.WriteString(`<p><a href="` + html.EscapeString(source.String()) + `">View interactive content in the original article.</a></p>`)
			continue
		default:
			return Article{}, fmt.Errorf("New York Times article contains an unsupported body block (%s)", cleanBlockType(block.Type))
		}
		body.WriteString("<" + tag + ">")
		for _, inline := range block.Content {
			if inline.Type != "TextInline" {
				return Article{}, fmt.Errorf("New York Times article contains unsupported inline content (%s)", cleanBlockType(inline.Type))
			}
			body.WriteString(html.EscapeString(inline.Text))
		}
		body.WriteString("</" + tag + ">")
	}
	body.WriteString("</article>")
	article, err := extractPage(body.String(), false, source)
	if err != nil {
		return Article{}, err
	}
	meta := metadata(doc, source)
	meta.Title, meta.Content, meta.Paragraphs = article.Title, article.Content, article.Paragraphs
	return meta, nil
}

// Report schema names without returning arbitrary publisher strings to the UI.
func cleanBlockType(value string) string {
	if len(value) == 0 || len(value) > 64 {
		return "unknown"
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return "unknown"
		}
	}
	return value
}
