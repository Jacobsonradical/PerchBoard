package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Jacobsonradical/PerchBoard/internal/config"
)

type fakeSummaryTransport func(*http.Request) (*http.Response, error)

func (f fakeSummaryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func summaryServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	return New(config.Paths{Dir: dir, LLMFile: filepath.Join(dir, "llm.json")}, nil)
}
func summaryCall(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestSummaryConfigIsolation(t *testing.T) {
	s := summaryServer(t)
	original := config.LLMConfig{Provider: "claude", Model: "filter-model", Key: "filter-secret"}
	if err := s.paths.SaveLLM(original); err != nil {
		t.Fatal(err)
	}
	w := summaryCall(s, "POST", "/api/summary/config", `{"provider":"openai","model":"summary-model","key":"summary-secret"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "summary-secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = summaryCall(s, "GET", "/api/summary/config", "")
	if strings.Contains(w.Body.String(), "secret") || !strings.Contains(w.Body.String(), "summary-model") {
		t.Fatal(w.Body.String())
	}
	st, err := os.Stat(filepath.Join(s.paths.Dir, "summary-llm.json"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("key file permissions", err)
	}
	w = summaryCall(s, "POST", "/api/summary/config", `{"provider":"openai","model":"other-model","key":""}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	saved, _ := s.summaryPaths().LoadLLM()
	if saved.Key != "summary-secret" {
		t.Fatal("same-provider key lost")
	}
	w = summaryCall(s, "POST", "/api/summary/config", `{"provider":"claude","model":"other-model","key":""}`)
	if w.Code != 400 {
		t.Fatal("cross-provider key reused")
	}
	w = summaryCall(s, "DELETE", "/api/summary/config", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	filtered, _ := s.paths.LoadLLM()
	if filtered != original {
		t.Fatal("smart filtering changed")
	}
	saved, _ = s.summaryPaths().LoadLLM()
	if saved.Key != "" {
		t.Fatal("summary key not removed")
	}
}
func TestSummaryCacheAndIndependentCredentials(t *testing.T) {
	s := summaryServer(t)
	s.paths.SaveLLM(config.LLMConfig{Provider: "openai", Model: "filter", Key: "filter-key"})
	in, _ := json.Marshal(map[string]any{"title": "Transit update", "paragraphs": []string{strings.Repeat("The city opened a new train line after a council vote. ", 15)}})
	if w := summaryCall(s, "POST", "/api/summary", string(in)); w.Code != 400 {
		t.Fatal("used filtering key")
	}
	summaryCall(s, "POST", "/api/summary/config", `{"provider":"openai","model":"summary-model","key":"summary-key"}`)
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	calls := 0
	http.DefaultTransport = fakeSummaryTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer summary-key" {
			t.Fatal("wrong credentials")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"sentences\":[\"The city opened a train line.\",\"The council approved the project.\"]}"}}]}`))}, nil
	})
	for i := 0; i < 2; i++ {
		if w := summaryCall(s, "POST", "/api/summary", string(in)); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatal("duplicate paid call", calls)
	}
	summaryCall(s, "POST", "/api/summary/config", `{"provider":"openai","model":"new-model","key":""}`)
	if w := summaryCall(s, "POST", "/api/summary", string(in)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if calls != 2 {
		t.Fatal("model change did not invalidate cache")
	}
	summaryCall(s, "DELETE", "/api/summary/config", "")
	if w := summaryCall(s, "POST", "/api/summary", string(in)); w.Code != 400 {
		t.Fatal("cache used after key removal")
	}
}
func TestSummaryRejectsCrossOriginAndInvalidRequests(t *testing.T) {
	s := summaryServer(t)
	r := httptest.NewRequest("POST", "/api/summary/config", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://unrelated.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("POST", "/api/summary", strings.NewReader(`{}`))
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
	for _, body := range []string{`{}`, `{"paragraphs":["Only a teaser."]}`, strings.Repeat("x", 300000)} {
		if w := summaryCall(s, "POST", "/api/summary", body); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
