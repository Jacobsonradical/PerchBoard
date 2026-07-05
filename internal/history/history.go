// Package history persists feed items over time so the dashboard doesn't lose
// track of articles that scroll off the source feed between visits.
//
// Each feed URL gets its own JSON file (named by a hash of the URL). New items
// are merged in by GUID — existing items keep their original "first seen" time —
// and the store is pruned by age and a max count so it stays bounded.
package history

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Item is a stored feed item. It mirrors the fields the RSS widget needs, plus
// FirstSeen — the unix time we first recorded it (used for ordering items that
// have no published date, and for age-based pruning).
type Item struct {
	GUID      string `json:"guid"`
	Title     string `json:"title"`
	Link      string `json:"link"`
	Source    string `json:"source"`
	Published string `json:"published"` // RFC3339, may be empty
	FirstSeen int64  `json:"firstSeen"` // unix seconds
}

// Store is a simple file-backed, mutex-guarded item store.
type Store struct {
	dir string
	mu  sync.Mutex
}

// New creates the store directory if needed.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// fileFor returns the per-URL storage path (hashed so any URL is filename-safe).
func (s *Store) fileFor(url string) string {
	sum := sha1.Sum([]byte(url))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:])+".json")
}

// Merge folds `fresh` items into the stored history for url, then prunes by age
// and caps the count. It returns the full merged history, newest first.
// Items already present keep their original FirstSeen; genuinely new items get
// FirstSeen = now.
func (s *Store) Merge(url string, fresh []Item, maxItems int, maxAge time.Duration) ([]Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byGUID := map[string]Item{}
	for _, it := range s.loadLocked(url) {
		if it.GUID != "" {
			byGUID[it.GUID] = it
		}
	}

	now := time.Now().Unix()
	for _, it := range fresh {
		if it.GUID == "" {
			continue
		}
		if existing, ok := byGUID[it.GUID]; ok {
			// Refresh mutable fields but preserve the original first-seen time.
			it.FirstSeen = existing.FirstSeen
		} else {
			it.FirstSeen = now
		}
		byGUID[it.GUID] = it
	}

	cutoff := now - int64(maxAge.Seconds())
	all := make([]Item, 0, len(byGUID))
	for _, it := range byGUID {
		if it.FirstSeen >= cutoff {
			all = append(all, it)
		}
	}
	sortNewestFirst(all)
	if maxItems > 0 && len(all) > maxItems {
		all = all[:maxItems]
	}

	if err := s.saveLocked(url, all); err != nil {
		return all, err
	}
	return all, nil
}

// Get returns the stored history for url (newest first), capped at maxItems.
// Used as a fallback when a live fetch fails so the widget still shows content.
func (s *Store) Get(url string, maxItems int) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.loadLocked(url)
	sortNewestFirst(all)
	if maxItems > 0 && len(all) > maxItems {
		all = all[:maxItems]
	}
	return all
}

// sortKey orders by published date, falling back to first-seen for undated items.
func sortKey(it Item) string {
	if it.Published != "" {
		return it.Published
	}
	return time.Unix(it.FirstSeen, 0).UTC().Format(time.RFC3339)
}

func sortNewestFirst(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		return sortKey(items[i]) > sortKey(items[j])
	})
}

// loadLocked reads the stored items for url (caller holds the lock). A missing
// or unreadable file yields an empty slice rather than an error.
func (s *Store) loadLocked(url string) []Item {
	data, err := os.ReadFile(s.fileFor(url))
	if err != nil {
		return nil
	}
	var items []Item
	if json.Unmarshal(data, &items) != nil {
		return nil
	}
	return items
}

// saveLocked writes items atomically (caller holds the lock).
func (s *Store) saveLocked(url string, items []Item) error {
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	path := s.fileFor(url)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
