// Package llm does one thing: ask an LLM to sort feed-item titles into the user's
// filter groups by the group's meaning, so a group titled "AI" can catch a headline
// that never says "AI" literally. It's an optional upgrade over the regex word match.
//
// Two providers are supported (Claude and OpenAI), both over plain net/http — a
// single classification call doesn't justify pulling in a full SDK, and one code
// path keeps the two providers consistent. The API key is passed in by the caller
// (the server reads it from local config); nothing here is stored or logged.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Item is a single thing to classify: a stable id (the feed GUID) + its title.
type Item struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

const (
	ProviderClaude = "claude"
	ProviderOpenAI = "openai"
)

// Reasonable, cheap defaults for a high-volume classification task. The user can
// override the model in settings, so the choice (and its cost) stays theirs.
func DefaultModel(provider string) string {
	if provider == ProviderOpenAI {
		return "gpt-4o-mini"
	}
	return "claude-haiku-4-5"
}

var httpClient = &http.Client{Timeout: 45 * time.Second}

// Classify returns, for each item id, the subset of groupTitles it belongs to.
// Titles that match nothing map to an empty slice. An unknown provider, a bad key,
// or an unparseable reply comes back as an error so the caller can fall back to the
// regex filter.
func Classify(ctx context.Context, provider, apiKey, model string, items []Item, groupTitles []string) (map[string][]string, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured")
	}
	if len(items) == 0 || len(groupTitles) == 0 {
		return map[string][]string{}, nil
	}
	if model == "" {
		model = DefaultModel(provider)
	}

	// Feed guids are long, opaque URLs that models echo back unreliably and can
	// pair with the wrong headline. Label items with short numeric ids in the
	// prompt, then map the model's answer back to the real guids ourselves.
	idToGuid := make(map[string]string, len(items))
	labeled := make([]Item, len(items))
	for i, it := range items {
		id := strconv.Itoa(i)
		idToGuid[id] = it.ID
		labeled[i] = Item{ID: id, Title: it.Title}
	}
	prompt := buildPrompt(labeled, groupTitles)

	var raw string
	var err error
	switch provider {
	case ProviderOpenAI:
		raw, err = callOpenAI(ctx, apiKey, model, prompt)
	case ProviderClaude, "":
		raw, err = callClaude(ctx, apiKey, model, prompt)
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	if err != nil {
		return nil, err
	}

	byID := parseResult(raw, groupTitles)
	out := make(map[string][]string, len(byID))
	for id, cats := range byID {
		if guid, ok := idToGuid[id]; ok {
			out[guid] = cats
		}
	}
	return out, nil
}

// buildPrompt asks for a strict JSON object mapping each id to an array of the
// category names it belongs to. Keeping the instruction terse and the output shape
// fixed makes the reply cheap and easy to parse across both providers.
func buildPrompt(items []Item, groups []string) string {
	var b strings.Builder
	b.WriteString("You sort news headlines into categories by topic.\n")
	b.WriteString("Categories (use these exact names): ")
	b.WriteString(strings.Join(quoteAll(groups), ", "))
	b.WriteString("\n\nEach item below is a numbered headline (\"<id>: <headline>\"). For each id, list the categories the headline is genuinely about — assign a category only when the headline is actually on that topic, not on a superficial word match. An item may match several categories or none.\n")
	b.WriteString("Reply with ONLY a JSON object whose keys are the item ids (as strings) and whose values are arrays of matching category names (exact strings from the list above); use [] when nothing fits. No prose, no code fences.\n\nItems:\n")
	// Compact, one item per line: numeric id then headline.
	for _, it := range items {
		title := it.Title
		if len(title) > 300 {
			title = title[:300]
		}
		fmt.Fprintf(&b, "%s: %s\n", it.ID, title)
	}
	return b.String()
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

// --- provider calls -------------------------------------------------------

func callClaude(ctx context.Context, apiKey, model, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 2048,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	res, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return "", providerError(res.StatusCode, data)
	}
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("unexpected Claude response")
	}
	var sb strings.Builder
	for _, c := range parsed.Content {
		sb.WriteString(c.Text)
	}
	return sb.String(), nil
}

func callOpenAI(ctx context.Context, apiKey, model, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":           model,
		"messages":        []map[string]string{{"role": "user", "content": prompt}},
		"response_format": map[string]string{"type": "json_object"},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	res, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return "", providerError(res.StatusCode, data)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil || len(parsed.Choices) == 0 {
		return "", fmt.Errorf("unexpected OpenAI response")
	}
	return parsed.Choices[0].Message.Content, nil
}

// providerError turns a non-200 into a short, safe message (the upstream body can
// echo request details, so we surface just the API's error text, not the request).
func providerError(status int, data []byte) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
		return fmt.Errorf("LLM API error (%d): %s", status, e.Error.Message)
	}
	return fmt.Errorf("LLM API error (%d)", status)
}

// --- parsing --------------------------------------------------------------

// parseResult pulls the JSON object out of the model's reply (tolerating stray
// prose or code fences) and keeps only category names that are actually in the
// user's group list, so a hallucinated label can't create a phantom section.
func parseResult(raw string, groups []string) map[string][]string {
	valid := make(map[string]bool, len(groups))
	for _, g := range groups {
		valid[g] = true
	}

	obj := extractJSONObject(raw)
	out := map[string][]string{}
	if obj == "" {
		return out
	}
	var m map[string][]string
	if err := json.Unmarshal([]byte(obj), &m); err != nil {
		return out
	}
	for id, cats := range m {
		kept := make([]string, 0, len(cats))
		for _, c := range cats {
			if valid[c] {
				kept = append(kept, c)
			}
		}
		out[id] = kept
	}
	return out
}

// extractJSONObject returns the substring from the first '{' to the last '}',
// which is enough to recover the object even if the model wrapped it in text.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}
