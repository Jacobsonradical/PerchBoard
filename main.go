// Command perchboard is the PerchBoard dashboard.
//
// Run modes (one binary, three ways to run — see README):
//   - default:           starts the local server and opens the app
//                        (native window if built with `-tags native`, else browser)
//   - --serve:           starts the server only, no window (for Docker / LAN sharing)
//   - --addr 0.0.0.0:7171  choose the bind address (Docker uses 0.0.0.0)
//
// The frontend is embedded into the binary so a single file is fully standalone.
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/Jacobsonradical/PerchBoard/internal/config"
	"github.com/Jacobsonradical/PerchBoard/internal/server"
)

// The built React app is embedded here. `web/dist` must exist at build time;
// `make build` (or `npm run build`) produces it. A placeholder ships in the repo
// so `go build` works before the frontend is built.
//
//go:embed all:web/dist
var embeddedWeb embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:7171", "address to bind the HTTP server")
	serveOnly := flag.Bool("serve", false, "serve only; do not open a window/browser")
	browser := flag.String("browser", "auto", "UI mode: firefox | chrome-app | default | auto")
	flag.Parse()

	paths, err := config.Resolve()
	if err != nil {
		log.Fatalf("perchboard: cannot resolve data dir: %v", err)
	}
	log.Printf("perchboard: data dir %s", paths.Dir)

	// Root the embedded FS at web/dist so paths are served from the app root.
	static, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		log.Fatalf("perchboard: embedded frontend missing: %v", err)
	}

	srv := server.New(paths, static)
	srv.StartPoller() // accumulate RSS history in the background
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Bind explicitly so we can log the real address (port 0 -> random).
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("perchboard: cannot listen on %s: %v", *addr, err)
	}
	url := "http://" + ln.Addr().String()
	log.Printf("perchboard: serving on %s", url)

	// Serve in the background; the foreground either launches the UI or blocks.
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("perchboard: server error: %v", err)
		}
	}()

	if *serveOnly {
		select {} // headless: just keep serving
	}
	launch(url, *browser) // open the UI (blocks)
}
