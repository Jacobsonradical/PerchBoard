// Package server wires the HTTP routes: JSON data APIs for each widget, the
// dashboard config get/save, background-image upload/serve, and finally the
// static React app (with SPA fallback).
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jacobsonradical/PerchBoard/internal/config"
	"github.com/Jacobsonradical/PerchBoard/internal/feeds"
	"github.com/Jacobsonradical/PerchBoard/internal/history"
	"github.com/Jacobsonradical/PerchBoard/internal/llm"
	"github.com/Jacobsonradical/PerchBoard/internal/scholarone"
)

// How long RSS history is kept and how many items per feed are retained.
const (
	rssHistoryMax = 500
	rssHistoryAge = 30 * 24 * time.Hour
	pollInterval  = 10 * time.Minute
)

// Server holds dependencies shared by the handlers.
type Server struct {
	paths  config.Paths
	static fs.FS          // the built React app (embedded or on-disk)
	hist   *history.Store // persistent RSS item history
}

// New builds the Server. static is the filesystem rooted at the React build
// output (containing index.html). It may be nil during early development.
func New(paths config.Paths, static fs.FS) *Server {
	// History lives alongside the rest of the data; failure here is non-fatal
	// (the dashboard still works without persistence).
	hist, err := history.New(filepath.Join(paths.Dir, "history"))
	if err != nil {
		hist = nil
	}
	return &Server{paths: paths, static: static, hist: hist}
}

// Handler returns the root http.Handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// --- data APIs (all key-free upstreams) ---
	mux.HandleFunc("/api/rss", s.handleRSS)
	mux.HandleFunc("/api/hn", s.handleHN)
	mux.HandleFunc("/api/weather", s.handleWeather)
	mux.HandleFunc("/api/geo", s.handleGeo)
	mux.HandleFunc("/api/geo/search", s.handleGeoSearch)
	mux.HandleFunc("/api/markets", s.handleMarkets)
	mux.HandleFunc("/api/stocknews", s.handleStockNews)
	mux.HandleFunc("/api/symbolsearch", s.handleSymbolSearch)

	// --- ScholarOne on-demand retrieval (drives a headless browser) ---
	mux.HandleFunc("/api/scholarone/retrieve", s.handleScholarOne)

	// --- optional LLM key config + smart RSS classification ---
	mux.HandleFunc("/api/llm/config", s.handleLLMConfig)
	mux.HandleFunc("/api/llm/test", s.handleLLMTest)
	mux.HandleFunc("/api/llm/classify", s.handleLLMClassify)

	// --- dashboard state ---
	mux.HandleFunc("/api/config", s.handleConfig)

	// --- background images ---
	mux.HandleFunc("/api/backgrounds", s.handleBackgrounds)
	mux.Handle("/backgrounds/", http.StripPrefix("/backgrounds/",
		http.FileServer(http.Dir(s.paths.Backgrounds))))

	// --- static React app + SPA fallback ---
	mux.HandleFunc("/", s.handleStatic)

	return withCommonHeaders(mux)
}

// ---- helpers -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr returns a JSON error so the frontend can show a clean message per
// widget instead of a blank box.
func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// joinNonEmpty joins the non-empty parts with ", " (for a location label).
func joinNonEmpty(parts ...string) string {
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

func intParam(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// withCommonHeaders disables caching of API responses (the widgets manage their
// own refresh) — static asset caching is left to the file server.
func withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// ---- data handlers -------------------------------------------------------

func (s *Server) handleRSS(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		writeErr(w, http.StatusBadRequest, errors.New("missing url param"))
		return
	}
	// No history store: behave like a plain live fetch.
	if s.hist == nil {
		items, err := feeds.FetchRSS(url)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, items)
		return
	}

	live, err := feeds.FetchRSS(url)
	if err != nil {
		// Upstream failed — still serve whatever history we have so the user
		// keeps seeing accumulated items.
		stored := s.hist.Get(url, rssHistoryMax)
		if len(stored) > 0 {
			writeJSON(w, fromHistory(stored))
			return
		}
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	merged, _ := s.hist.Merge(url, toHistory(live), rssHistoryMax, rssHistoryAge)
	writeJSON(w, fromHistory(merged))
}

// toHistory / fromHistory convert between the feed item shape and the stored
// history item shape.
func toHistory(items []feeds.RSSItem) []history.Item {
	out := make([]history.Item, len(items))
	for i, it := range items {
		out[i] = history.Item{GUID: it.GUID, Title: it.Title, Link: it.Link, Source: it.Source, Published: it.Published}
	}
	return out
}

func fromHistory(items []history.Item) []feeds.RSSItem {
	out := make([]feeds.RSSItem, len(items))
	for i, it := range items {
		out[i] = feeds.RSSItem{GUID: it.GUID, Title: it.Title, Link: it.Link, Source: it.Source, Published: it.Published}
	}
	return out
}

// StartPoller periodically fetches every RSS feed referenced in the saved config
// and merges new items into history — so the dashboard keeps catching news even
// while no browser is open. Safe to call once at startup; runs in the background.
func (s *Server) StartPoller() {
	if s.hist == nil {
		return
	}
	go func() {
		s.pollFeeds() // prime immediately on startup
		t := time.NewTicker(pollInterval)
		defer t.Stop()
		for range t.C {
			s.pollFeeds()
		}
	}()
}

// pollFeeds reads the saved dashboard config, collects the unique RSS feed URLs
// across all RSS widgets, fetches each, and merges into history.
func (s *Server) pollFeeds() {
	data, err := s.paths.LoadConfig()
	if err != nil || data == nil {
		return
	}
	// Feeds may be plain URL strings (old configs) or {url, name} objects (new),
	// so decode each entry as raw JSON and accept either form.
	var cfg struct {
		Widgets []struct {
			Type     string `json:"type"`
			Settings struct {
				Feeds []json.RawMessage `json:"feeds"`
			} `json:"settings"`
		} `json:"widgets"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return
	}
	seen := map[string]bool{}
	for _, wgt := range cfg.Widgets {
		if wgt.Type != "rss" {
			continue
		}
		for _, raw := range wgt.Settings.Feeds {
			u := feedURL(raw)
			if u == "" || seen[u] {
				continue
			}
			seen[u] = true
			if items, err := feeds.FetchRSS(u); err == nil {
				s.hist.Merge(u, toHistory(items), rssHistoryMax, rssHistoryAge)
			}
		}
	}
}

// feedURL extracts a feed URL from a config entry that is either a plain string
// ("https://…") or an object ({"url":"https://…","name":"…"}).
func feedURL(raw json.RawMessage) string {
	var u string
	if json.Unmarshal(raw, &u) == nil {
		return u
	}
	var obj struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.URL
	}
	return ""
}

func (s *Server) handleHN(w http.ResponseWriter, r *http.Request) {
	items, err := feeds.FetchHN(intParam(r, "limit", 30))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, items)
}

func (s *Server) handleWeather(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	latStr, lonStr := q.Get("lat"), q.Get("lon")
	var lat, lon float64
	var place string
	if latStr == "" || lonStr == "" {
		// No coordinates supplied -> fall back to IP geolocation, and use the
		// resolved city as the displayed location name.
		loc, err := feeds.AutoLocate()
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		lat, lon = loc.Latitude, loc.Longitude
		place = joinNonEmpty(loc.City, loc.Region, loc.Country)
	} else {
		lat, _ = strconv.ParseFloat(latStr, 64)
		lon, _ = strconv.ParseFloat(lonStr, 64)
	}
	wx, err := feeds.FetchWeather(lat, lon, q.Get("temp"), q.Get("wind"))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	// Copy before attaching the per-request place (FetchWeather returns a shared
	// cached value).
	out := *wx
	out.Place = place
	writeJSON(w, out)
}

func (s *Server) handleGeo(w http.ResponseWriter, r *http.Request) {
	loc, err := feeds.AutoLocate()
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, loc)
}

func (s *Server) handleGeoSearch(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("q")
	if name == "" {
		writeErr(w, http.StatusBadRequest, errors.New("missing q param"))
		return
	}
	res, err := feeds.SearchPlace(name)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleMarkets(w http.ResponseWriter, r *http.Request) {
	symbols := strings.Split(r.URL.Query().Get("symbols"), ",")
	quotes, err := feeds.FetchMarkets(symbols)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, quotes)
}

func (s *Server) handleStockNews(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		writeErr(w, http.StatusBadRequest, errors.New("missing symbol param"))
		return
	}
	news, err := feeds.FetchStockNews(symbol)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, news)
}

func (s *Server) handleSymbolSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeErr(w, http.StatusBadRequest, errors.New("missing q param"))
		return
	}
	res, err := feeds.SearchSymbols(q)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, res)
}

// ---- ScholarOne ----------------------------------------------------------

// handleScholarOne retrieves paper/review status for one or more ScholarOne
// sites. Credentials arrive in the POST body, are passed straight to the
// retriever (which uses them only to fill the in-memory login form), and are
// never persisted or logged. The response carries only the scraped, non-secret
// results so the widget can cache them client-side.
func (s *Server) handleScholarOne(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Sites []scholarone.SiteCreds `json:"sites"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Sites) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("no sites to retrieve"))
		return
	}
	// One generous ceiling for the whole batch; each site also caps itself.
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	results := scholarone.Retrieve(ctx, req.Sites)
	writeJSON(w, map[string]any{"results": results})
}

// handleLLMConfig gets / sets / clears the optional LLM API key used for smart RSS
// filtering. GET never returns the key itself — only whether one is configured and
// which provider/model — so the secret stays server-side.
func (s *Server) handleLLMConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c, _ := s.paths.LoadLLM()
		writeJSON(w, map[string]any{
			"configured": c.Key != "",
			"provider":   c.Provider,
			"model":      c.Model,
		})
	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<10))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		var in config.LLMConfig
		if err := json.Unmarshal(body, &in); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		// Let the user change provider/model without re-typing the key: an empty
		// key on save keeps the existing one.
		if in.Key == "" {
			if cur, _ := s.paths.LoadLLM(); cur.Key != "" {
				in.Key = cur.Key
			}
		}
		if err := s.paths.SaveLLM(in); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]any{"configured": in.Key != "", "provider": in.Provider, "model": in.Model})
	case http.MethodDelete:
		if err := s.paths.ClearLLM(); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]any{"configured": false})
	default:
		writeErr(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

// handleLLMTest does one tiny classification against the saved key so the UI can
// tell the user whether the LLM connection actually works.
func (s *Server) handleLLMTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	cfg, _ := s.paths.LoadLLM()
	if cfg.Key == "" {
		writeErr(w, http.StatusBadRequest, errors.New("no LLM API key configured"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if _, err := llm.Classify(ctx, cfg.Provider, cfg.Key, cfg.Model,
		[]llm.Item{{ID: "0", Title: "A new AI model was released today."}}, []string{"AI"}); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "provider": cfg.Provider, "model": cfg.Model})
}

// handleLLMClassify sorts the given item titles into the given filter-group titles
// using the configured LLM. The key is read from local config, never from the
// request, so the browser never handles it.
func (s *Server) handleLLMClassify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	cfg, _ := s.paths.LoadLLM()
	if cfg.Key == "" {
		writeErr(w, http.StatusBadRequest, errors.New("no LLM API key configured"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Items  []llm.Item `json:"items"`
		Groups []string   `json:"groups"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	result, err := llm.Classify(ctx, cfg.Provider, cfg.Key, cfg.Model, req.Items, req.Groups)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, map[string]any{"classifications": result})
}

// ---- config --------------------------------------------------------------

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := s.paths.LoadConfig()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if data == nil {
			// No saved config yet: empty object, frontend applies defaults.
			_, _ = w.Write([]byte("{}"))
			return
		}
		_, _ = w.Write(data)
	case http.MethodPut, http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		// Validate it is JSON before persisting.
		if !json.Valid(body) {
			writeErr(w, http.StatusBadRequest, errors.New("body is not valid JSON"))
			return
		}
		if err := s.paths.SaveConfig(body); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

// ---- backgrounds ---------------------------------------------------------

// handleBackgrounds lists (GET), uploads (POST multipart "image"), or deletes
// (DELETE ?name=) background images.
func (s *Server) handleBackgrounds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		entries, err := os.ReadDir(s.paths.Backgrounds)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		names := []string{}
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		writeJSON(w, names)

	case http.MethodPost:
		file, hdr, err := r.FormFile("image")
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		defer file.Close()
		// Generate a collision-free name, keep the original extension.
		ext := strings.ToLower(filepath.Ext(hdr.Filename))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif":
		default:
			writeErr(w, http.StatusBadRequest, errors.New("unsupported image type"))
			return
		}
		buf := make([]byte, 8)
		_, _ = rand.Read(buf)
		name := hex.EncodeToString(buf) + ext
		dst, err := os.Create(filepath.Join(s.paths.Backgrounds, name))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		defer dst.Close()
		if _, err := io.Copy(dst, io.LimitReader(file, 20<<20)); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]string{"name": name, "url": "/backgrounds/" + name})

	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		// Guard against path traversal: only a bare filename is allowed.
		if name == "" || name != filepath.Base(name) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid name"))
			return
		}
		if err := os.Remove(filepath.Join(s.paths.Backgrounds, name)); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})

	default:
		writeErr(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
	}
}

// ---- static frontend -----------------------------------------------------

// handleStatic serves the built React app. Unknown non-asset paths fall back to
// index.html so client-side routing / deep links work (SPA behaviour).
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.static == nil {
		http.Error(w, "frontend not built yet — run `npm run build` in web/", http.StatusServiceUnavailable)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	if f, err := s.static.Open(p); err == nil {
		f.Close()
		http.FileServer(http.FS(s.static)).ServeHTTP(w, r)
		return
	}
	// Fallback: serve index.html for any unmatched route.
	r.URL.Path = "/"
	http.FileServer(http.FS(s.static)).ServeHTTP(w, r)
}
