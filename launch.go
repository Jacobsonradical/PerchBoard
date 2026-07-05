package main

import (
	"log"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/Jacobsonradical/PerchBoard/internal/config"
)

// launch opens PerchBoard's UI. `mode` selects how:
//
//	"firefox"     - open in Firefox (a normal window/tab). Feed links open as new
//	                Firefox tabs you can bookmark/save — this is the default.
//	"chrome-app"  - a chromeless Chromium "app-mode" window (most app-like, but
//	                links open in Chromium, not your main browser).
//	"default"     - your OS default browser.
//	"auto"        - Firefox if present, else chrome-app, else default.
//
// launch blocks so the server lives as long as the UI session.
func launch(url, mode string) {
	switch mode {
	case "firefox":
		if openFirefox(url) {
			keepAlive()
		}
		fallbackBrowser(url)
	case "chrome-app":
		if openChromeApp(url) {
			return // chrome-app runs as its own process; exits when window closes
		}
		fallbackBrowser(url)
	case "default":
		fallbackBrowser(url)
	default: // auto
		if openFirefox(url) {
			keepAlive()
		}
		if openChromeApp(url) {
			return
		}
		fallbackBrowser(url)
	}
}

// openFirefox opens url in a new Firefox window. Returns false if Firefox is not
// installed. Note: when Firefox is already running it delegates to that instance
// and the spawned process exits immediately, so callers keep the server alive
// separately (keepAlive).
func openFirefox(url string) bool {
	path, err := lookPath("firefox", "firefox-esr")
	if err != nil {
		return false
	}
	log.Printf("perchboard: opening in Firefox — close the window and press Ctrl+C to stop the server")
	_ = exec.Command(path, "--new-window", url).Start()
	return true
}

// openChromeApp opens a chromeless Chromium app-mode window and blocks until it
// closes. Returns false if no Chromium-family browser is installed.
func openChromeApp(url string) bool {
	path, err := lookPath("google-chrome", "google-chrome-stable", "chromium",
		"chromium-browser", "brave-browser", "microsoft-edge", "vivaldi-stable")
	if err != nil {
		return false
	}
	profile := filepath.Join(dataDir(), "browser-profile")
	log.Printf("perchboard: opening standalone window via %s", filepath.Base(path))
	cmd := exec.Command(path,
		"--app="+url,
		"--user-data-dir="+profile,
		"--no-first-run", "--no-default-browser-check",
		"--window-size=1280,820",
	)
	if err := cmd.Start(); err != nil {
		return false
	}
	_ = cmd.Wait()
	return true
}

// fallbackBrowser opens the OS default browser, then blocks to keep serving.
func fallbackBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, append(args, url)...).Start()
	keepAlive()
}

func keepAlive() { select {} }

// lookPath returns the first candidate found on PATH.
func lookPath(candidates ...string) (string, error) {
	var lastErr error
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		} else {
			lastErr = err
		}
	}
	return "", lastErr
}

func dataDir() string {
	if p, err := config.Resolve(); err == nil {
		return p.Dir
	}
	return "/tmp"
}
