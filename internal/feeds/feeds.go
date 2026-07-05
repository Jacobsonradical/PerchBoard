// Package feeds contains the upstream data integrations (RSS, Hacker News,
// Weather, Markets). Each integration is a small, independent file.
//
// Two shared concerns live here:
//   - a single HTTP client with a sane timeout and a polite User-Agent
//     (some upstreams reject the default Go agent), and
//   - a tiny in-memory TTL cache so the dashboard polling several widgets does
//     not hammer upstream APIs.
package feeds

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// UserAgent identifies us to upstream APIs (some reject the default Go agent).
const UserAgent = "PerchBoard/0.1 (personal dashboard; +https://github.com/Jacobsonradical/PerchBoard)"

// httpClient is shared by all integrations.
var httpClient = &http.Client{Timeout: 12 * time.Second}

// getBytes performs a GET with our User-Agent and returns the body bytes,
// erroring on non-2xx so callers can surface a clean message to the widget.
func getBytes(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json, text/xml, application/xml, */*")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // cap at 8MB
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream %s returned %d", url, resp.StatusCode)
	}
	return body, nil
}

// getJSON fetches and decodes JSON into v.
func getJSON(url string, v any) error {
	body, err := getBytes(url)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// --- tiny TTL cache -------------------------------------------------------

type cacheEntry struct {
	value   any
	expires time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

// cached returns a cached value for key if still fresh, otherwise calls produce,
// stores the result for ttl, and returns it. produce errors are not cached.
func cached(key string, ttl time.Duration, produce func() (any, error)) (any, error) {
	cacheMu.Lock()
	if e, ok := cache[key]; ok && time.Now().Before(e.expires) {
		cacheMu.Unlock()
		return e.value, nil
	}
	cacheMu.Unlock()

	v, err := produce()
	if err != nil {
		return nil, err
	}
	cacheMu.Lock()
	cache[key] = cacheEntry{value: v, expires: time.Now().Add(ttl)}
	cacheMu.Unlock()
	return v, nil
}
