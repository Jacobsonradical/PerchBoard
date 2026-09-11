package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Jacobsonradical/PerchBoard/internal/config"
	"github.com/Jacobsonradical/PerchBoard/internal/llm"
)

type summaryEntry struct {
	Sentences []string
	Expires   time.Time
}

type summaryState struct {
	configMu sync.Mutex
	gate     chan struct{}
	cache    map[[32]byte]summaryEntry
}

func (s *Server) summaryPaths() config.Paths {
	p := s.paths
	p.LLMFile = filepath.Join(p.Dir, "summary-llm.json")
	return p
}

func (s *Server) handleSummaryConfig(w http.ResponseWriter, r *http.Request) {
	if !allowSummaryRequest(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.summary.configMu.Lock()
	defer s.summary.configMu.Unlock()
	p := s.summaryPaths()
	c, err := p.LoadLLM()
	if err != nil {
		writeErr(w, 500, errors.New("could not load summary settings"))
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var in config.LLMConfig
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in) != nil {
			writeErr(w, 400, errors.New("invalid summary settings"))
			return
		}
		in.Provider, in.Model, in.Key = strings.TrimSpace(in.Provider), strings.TrimSpace(in.Model), strings.TrimSpace(in.Key)
		if (in.Provider != llm.ProviderClaude && in.Provider != llm.ProviderOpenAI) || in.Model == "" || len(in.Model) > 200 || strings.ContainsAny(in.Model, "\r\n") {
			writeErr(w, 400, errors.New("choose Claude or OpenAI and enter a model ID"))
			return
		}
		if in.Key == "" && in.Provider == c.Provider {
			in.Key = c.Key
		}
		if in.Key == "" || strings.ContainsAny(in.Key, "\r\n") {
			writeErr(w, 400, errors.New("enter an API key for this summary provider"))
			return
		}
		if p.SaveLLM(in) != nil {
			writeErr(w, 500, errors.New("could not save summary settings"))
			return
		}
		c = in
	case http.MethodDelete:
		if p.ClearLLM() != nil {
			writeErr(w, 500, errors.New("could not remove summary settings"))
			return
		}
		c = config.LLMConfig{}
	default:
		writeErr(w, 405, errors.New("method not allowed"))
		return
	}
	writeJSON(w, map[string]any{"configured": c.Key != "", "provider": c.Provider, "model": c.Model})
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	if !allowSummaryRequest(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeErr(w, 405, errors.New("method not allowed"))
		return
	}
	var in llm.SummaryInput
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&in) != nil {
		writeErr(w, 400, errors.New("invalid or oversized article text"))
		return
	}
	if err := in.Validate(); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.summary.configMu.Lock()
	c, err := s.summaryPaths().LoadLLM()
	s.summary.configMu.Unlock()
	if err != nil {
		writeErr(w, 500, errors.New("could not load summary settings"))
		return
	}
	if c.Key == "" {
		writeErr(w, 400, errors.New("set up AI article summary in Settings; smart filtering uses a separate key"))
		return
	}
	// Include credentials and the exact text in the digest to invalidate cached
	// results after provider/model/key changes without storing keys in the cache.
	data, _ := json.Marshal(struct {
		Config config.LLMConfig
		Input  llm.SummaryInput
	}{c, in})
	id := sha256.Sum256(data)
	// Serialize summary calls to coalesce repeated opens and bound API concurrency.
	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()
	select {
	case s.summary.gate <- struct{}{}:
		defer func() { <-s.summary.gate }()
	case <-ctx.Done():
		writeErr(w, http.StatusGatewayTimeout, errors.New("summary request timed out; please try again"))
		return
	}
	entry, ok := s.summary.cache[id]
	if !ok || time.Now().After(entry.Expires) {
		sentences, err := llm.Summarize(ctx, c.Provider, c.Key, c.Model, in)
		if err != nil {
			writeErr(w, 502, err)
			return
		}
		if len(s.summary.cache) >= 128 || s.summary.cache == nil {
			s.summary.cache = make(map[[32]byte]summaryEntry)
		}
		entry = summaryEntry{sentences, time.Now().Add(time.Hour)}
		s.summary.cache[id] = entry
	}
	writeJSON(w, map[string]any{"sentences": entry.Sentences, "provider": c.Provider, "model": c.Model})
}

// Browser requests that can save credentials or spend API quota must originate
// from this app. JSON also prevents cross-origin form submissions.
func allowSummaryRequest(w http.ResponseWriter, r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
			writeErr(w, http.StatusForbidden, errors.New("cross-origin summary requests are not allowed"))
			return false
		}
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeErr(w, http.StatusForbidden, errors.New("cross-site summary requests are not allowed"))
		return false
	}
	if r.Method == http.MethodPost && strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		writeErr(w, http.StatusUnsupportedMediaType, errors.New("send summary requests as application/json"))
		return false
	}
	return true
}
