package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type summaryTransport func(*http.Request) (*http.Response, error)

func (f summaryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func summaryFixture() SummaryInput {
	return SummaryInput{Title: "A new rail line opens", Paragraphs: []string{strings.Repeat("The city opened a rail line serving three districts after a council vote. ", 12)}}
}
func summaryResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestSummaryProviders(t *testing.T) {
	old := summaryClient
	t.Cleanup(func() { summaryClient = old })
	for _, provider := range []string{ProviderClaude, ProviderOpenAI} {
		t.Run(provider, func(t *testing.T) {
			summaryClient = &http.Client{Transport: summaryTransport(func(r *http.Request) (*http.Response, error) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if string(body["model"]) != `"chosen-model"` {
					t.Fatal("model not preserved")
				}
				var messages []map[string]string
				json.Unmarshal(body["messages"], &messages)
				var input SummaryInput
				if err := json.Unmarshal([]byte(messages[len(messages)-1]["content"]), &input); err != nil || input.Paragraphs[0] != summaryFixture().Paragraphs[0] {
					t.Fatal("article text not preserved as data")
				}
				if provider == ProviderClaude {
					if r.URL.Host != "api.anthropic.com" || r.Header.Get("x-api-key") != "test-summary-key" || len(body["system"]) == 0 {
						t.Fatal("incorrect Claude request")
					}
					return summaryResponse(`{"stop_reason":"end_turn","content":[{"type":"thinking","text":"not a summary"},{"type":"text","text":"{\"sentences\":[\"The city opened a rail line.\",\"It serves three districts.\"]}"}]}`), nil
				}
				if r.URL.Host != "api.openai.com" || r.Header.Get("Authorization") != "Bearer test-summary-key" || messages[0]["role"] != "system" || string(body["store"]) != "false" {
					t.Fatal("incorrect OpenAI request")
				}
				return summaryResponse(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"sentences\":[\"The city opened a rail line.\",\"It serves three districts.\"]}"}}]}`), nil
			})}
			got, err := Summarize(context.Background(), provider, "test-summary-key", "chosen-model", summaryFixture())
			if err != nil || len(got) != 2 {
				t.Fatalf("%v %v", got, err)
			}
		})
	}
}

func TestSummaryRejectsInvalidOrTruncatedReplies(t *testing.T) {
	old := summaryClient
	t.Cleanup(func() { summaryClient = old })
	for _, response := range []string{
		`{"choices":[{"finish_reason":"length","message":{"content":"{\"sentences\":[\"One.\",\"Two.\"]}"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"{\"sentences\":[\"One.\"]}"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"{\"sentences\":[\"One.\",\"\"]}"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"refusal":"Refused","content":""}}]}`,
		`not JSON`,
	} {
		summaryClient = &http.Client{Transport: summaryTransport(func(*http.Request) (*http.Response, error) { return summaryResponse(response), nil })}
		if _, err := Summarize(context.Background(), ProviderOpenAI, "key", "model", summaryFixture()); err == nil {
			t.Fatalf("accepted %s", response)
		}
	}
	summaryClient = &http.Client{Transport: summaryTransport(func(*http.Request) (*http.Response, error) {
		r := summaryResponse(`{"error":{"message":"secret-test-key"}}`)
		r.StatusCode = 401
		return r, nil
	})}
	_, err := Summarize(context.Background(), ProviderClaude, "secret-test-key", "model", summaryFixture())
	if err == nil || strings.Contains(err.Error(), "secret-test-key") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestSummaryRejectsInsufficientAndOversizedTextBeforeCallingProvider(t *testing.T) {
	old := summaryClient
	t.Cleanup(func() { summaryClient = old })
	summaryClient = &http.Client{Transport: summaryTransport(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected paid call"); return nil, nil })}
	for _, text := range []string{"A title is not an article.", strings.Repeat("文", 60001)} {
		if _, err := Summarize(context.Background(), ProviderClaude, "key", "model", SummaryInput{Paragraphs: []string{text}}); err == nil {
			t.Fatal("invalid text accepted")
		}
	}
}
