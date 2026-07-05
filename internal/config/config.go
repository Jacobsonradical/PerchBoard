// Package config handles where PerchBoard keeps its data on disk and provides
// helpers to load/save the dashboard state and uploaded background images.
//
// Design choice (simplicity first): the backend does NOT model the dashboard.
// It persists an opaque JSON document that the React frontend fully owns. This
// keeps the Go side dumb and means new widget settings never require backend
// changes. Backgrounds are the one exception — they are binary files, so they
// get their own small file-based store.
package config

import (
	"os"
	"path/filepath"
)

// Paths bundles the resolved on-disk locations PerchBoard uses.
type Paths struct {
	Dir         string // base data dir, e.g. ~/.config/perchboard
	ConfigFile  string // dashboard state JSON
	Backgrounds string // directory holding uploaded background images
}

// Resolve decides where data lives. We honour PERCHBOARD_DATA for Docker /
// custom setups, otherwise fall back to the OS user-config dir. Everything is
// created on demand so first run "just works".
func Resolve() (Paths, error) {
	base := os.Getenv("PERCHBOARD_DATA")
	if base == "" {
		ucd, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, err
		}
		base = filepath.Join(ucd, "perchboard")
	}
	p := Paths{
		Dir:         base,
		ConfigFile:  filepath.Join(base, "config.json"),
		Backgrounds: filepath.Join(base, "backgrounds"),
	}
	// Ensure both the base dir and the backgrounds dir exist.
	if err := os.MkdirAll(p.Backgrounds, 0o755); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// LoadConfig returns the raw dashboard JSON. If no config has been saved yet it
// returns nil (the caller serves an empty/default document) rather than error.
func (p Paths) LoadConfig() ([]byte, error) {
	data, err := os.ReadFile(p.ConfigFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

// SaveConfig writes the dashboard JSON atomically (write temp + rename) so a
// crash mid-write can never corrupt the user's layout.
func (p Paths) SaveConfig(data []byte) error {
	tmp := p.ConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p.ConfigFile)
}
