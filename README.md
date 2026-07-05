# PerchBoard

A personal, customizable home dashboard — inspired by
[Glance](https://github.com/glanceapp/glance), but trimmed down and tailored to
the widgets and interactions I actually want.

**Widgets:** Clock · Weather · RSS · Hacker News · Markets · Calendar · ScholarOne.

Everything is customized **in the UI** — no config files to edit. Add feeds and
tickers with a button, drag widgets around, drop in background photos (the text
auto-adjusts for contrast), and pick fonts/units.

---

# Getting started

There are two ways to run PerchBoard. Pick one:

- **Option A — Docker** (recommended): one command, runs anywhere, auto-starts on
  reboot. Best if you just want to *use* it.
- **Option B — Standalone**: build it from source and run the binary directly.
  Best if you want to hack on it or don't want Docker.

Both serve the dashboard at **http://localhost:7171**.

---

## Option A — Docker

### Step 1 — Install Docker

- **Windows / macOS:** download **Docker Desktop** from
  <https://www.docker.com/products/docker-desktop/> and install it. On Windows it
  runs on the **WSL 2** backend — the installer sets this up for you (accept the
  "Use WSL 2" option if asked; on older builds, enable WSL 2 first with
  `wsl --install` in an admin PowerShell, then reboot).
- **Linux (Ubuntu):** install Docker Engine following
  <https://docs.docker.com/engine/install/ubuntu/>.

### Step 2 — Make sure Docker is running

Run:

```bash
docker info
```

- If you see a **`Server:`** section with details → Docker is running. ✅
- If you see `Cannot connect to the Docker daemon…` → start it:
  - Windows/macOS: open the **Docker Desktop** app and wait for "running".
  - Linux: `sudo systemctl enable --now docker` (this also makes it start on boot).

> **About `sudo` (Linux):** if `docker` commands say *"permission denied"*, you
> have two choices:
> 1. **Prefix every command with `sudo`** (e.g. `sudo docker compose up -d`), or
> 2. **Add yourself to the docker group once** so you never need `sudo`:
>    ```bash
>    sudo usermod -aG docker $USER
>    newgrp docker        # or just log out and back in
>    ```
> All commands below work with or without `sudo` — add it if needed.

### Step 3 — Start PerchBoard

**Easiest (uses the prebuilt image — no download of source code):**

```bash
mkdir -p ~/perchboard && cd ~/perchboard
curl -sL https://raw.githubusercontent.com/Jacobsonradical/PerchBoard/main/docker-compose.yml -o docker-compose.yml
docker compose up -d
```

That makes a folder `~/perchboard`, drops a `docker-compose.yml` into it, and
starts the container in the background.

> **On Windows:** the commands above work as-is in a **WSL 2** shell or **Git
> Bash**. In native **PowerShell**, use these equivalents:
>
> ```powershell
> mkdir $HOME\perchboard; cd $HOME\perchboard
> curl.exe -sL https://raw.githubusercontent.com/Jacobsonradical/PerchBoard/main/docker-compose.yml -o docker-compose.yml
> docker compose up -d
> ```
>
> (`curl.exe` is the real curl bundled with Windows 10/11 — plain `curl` in
> PowerShell is an alias for a different command, so the `.exe` matters here.)
> Everything after this — `docker ps`, `logs`, `compose pull`, etc. — is the same
> on every platform.

**Or build it yourself from source** (if the prebuilt image isn't available):

```bash
git clone https://github.com/Jacobsonradical/PerchBoard.git
cd PerchBoard
docker build -t perchboard:latest .
docker run -d --name perchboard --restart unless-stopped \
  -p 127.0.0.1:7171:7171 -v perchboard-data:/data perchboard:latest
```

What the `docker run` flags mean:
- `-d` — run in the background.
- `--name perchboard` — name it so it's easy to manage.
- `--restart unless-stopped` — **auto-start on every reboot** (see below).
- `-p 127.0.0.1:7171:7171` — serve it on your machine's port 7171, reachable
  from this machine only (not from other devices on your network). Drop the
  `127.0.0.1:` prefix only if you deliberately want to open it to your LAN.
- `-v perchboard-data:/data` — save your dashboard + backgrounds in a volume so
  they survive restarts and updates. This is a Docker-managed **named volume**, so
  it works the same on Windows, macOS, and Linux — there are no host folder paths
  to translate (nothing like `C:\...` to get right).

### Step 4 — Open it

Go to **http://localhost:7171** in your browser.

### Managing it

```bash
docker ps                     # is it running? look for "perchboard"
docker logs -f perchboard     # view logs (Ctrl+C to stop watching)
docker stop perchboard        # stop
docker start perchboard       # start again
docker rm -f perchboard       # remove the container (your data stays in the volume)
```

### Auto-start on reboot

Two things make this work, and both are already handled above:
1. The Docker service starts on boot — `sudo systemctl enable --now docker` (Linux),
   or enable "Start Docker Desktop when you log in" in Docker Desktop settings.
2. The container has a restart policy — `--restart unless-stopped` (the
   `docker run` above) or `restart: unless-stopped` (the compose file).

After a reboot, check with `docker ps` — it should already be `Up`.

### Updating later

```bash
# prebuilt-image method:
cd ~/perchboard && docker compose pull && docker compose up -d

# from-source method:
cd PerchBoard && git pull && docker build -t perchboard:latest . \
  && docker rm -f perchboard \
  && docker run -d --name perchboard --restart unless-stopped \
     -p 127.0.0.1:7171:7171 -v perchboard-data:/data perchboard:latest
```

Your config and backgrounds live in the `perchboard-data` volume, so they
survive updates.

---

## Option B — Standalone (build from source)

### Step 1 — Install the tools

You need **Go ≥ 1.25** and **Node.js ≥ 20**.

- **Go:** download from <https://go.dev/dl/> (or `sudo apt install golang-go` on
  recent Ubuntu — check `go version` is ≥ 1.25).
- **Node.js:** download from <https://nodejs.org/> (or use
  [nvm](https://github.com/nvm-sh/nvm): `nvm install 20`).

Verify:

```bash
go version     # should print go1.25 or newer
node --version # should print v20 or newer
```

### Step 2 — Get the code

```bash
git clone https://github.com/Jacobsonradical/PerchBoard.git
cd PerchBoard
```

### Step 3 — Build

```bash
make build
```

This installs the frontend dependencies, builds the web UI, and compiles
everything into a single program called `perchboard` in the current folder.

### Step 4 — Run

```bash
./perchboard                       # opens in Firefox (default)
./perchboard --browser chrome-app  # chromeless app-style window instead
./perchboard --serve               # just serve; open http://localhost:7171 yourself
```

`--browser` accepts `firefox` | `chrome-app` | `default` | `auto`.

Your settings are saved to `~/.config/perchboard/` (override with the
`PERCHBOARD_DATA` environment variable).

---

# Using the dashboard

- **＋ Add widget** — pick any component; it drops onto the grid.
- **✥ Arrange** — drag widgets by their header, resize from the corner, rename
  inline, or remove. The layout saves automatically.
- **⚙ Settings** — choose a font and text size, upload background photos
  (static or shuffle), and enable OS notifications.
- **RSS** — add feed URLs and optional "filter words". A filter word (e.g. `AI`)
  splits the feed into an `AI` category and `Other` by title; categories collapse.
  Set the max length, click ✕ to permanently ignore an item, read items dim, and
  new items raise a notification.
- **Weather** — auto-detects your location (or search a place); shows the current
  temperature in both °C and °F, an hourly strip, and a past+future daily forecast
  with wind, rain %, and sunrise/sunset.
- **Markets** — one tab per ticker; each shows price, an interactive intraday
  chart (hover for price + time), key stats, and company-specific news. Add a
  stock by searching its name (e.g. "Nvidia" → NVDA).
- **Clock** — an analog dial set in a starfield, with the live digital time.
- **ScholarOne** — check your paper (Author) and review (Reviewer) status across
  ScholarOne journal sites (e.g. ISR, Management Science, MIS Quarterly) without
  logging into each one by hand. Enter your login(s) — one set for all sites or
  per-site — and PerchBoard drives a headless browser locally to read each
  dashboard, then groups the results per journal with a Paper/Review toggle.
  Credentials are used once and never stored. Needs Chrome/Chromium installed
  (the Docker image bundles Chromium).

---

# How it works

A single Go binary serves a small REST API **and** the embedded React app, so
there's nothing else to install. It runs three ways: a standalone window, a plain
web app (`--serve`), or in Docker.

- **Backend:** Go. Uses only **key-free** data sources so anyone can run it:
  Open-Meteo (weather + geocoding), Hacker News API, Yahoo Finance (quotes,
  news, symbol search), ipapi (IP geolocation), and any RSS/Atom feed.
- **Frontend:** React + Vite, `react-grid-layout` for drag/resize.
- **Persistence:** the whole dashboard is one JSON document the backend stores
  verbatim (`config.json`); backgrounds are saved as files. Both live in
  `~/.config/perchboard/`, or `/data` inside Docker.

```
main.go            entry: starts the server, opens the window/browser
launch.go          standalone window (Firefox by default; chrome-app for a chromeless window)
internal/server/   HTTP routes (data APIs, config, background upload, static SPA)
internal/feeds/    upstream integrations (rss, hn, weather, markets, news, geo) + cache
internal/config/   on-disk paths + atomic config save
web/               React frontend (built into web/dist, embedded into the binary)
```

### Development (hot reload)

```bash
make dev-api   # Go backend on :7171
make dev-web   # Vite dev server on :5173 (proxies /api to the backend)
```

---

# Notes & limitations

- **Notifications** are a browser feature, so they only fire while the page is
  open (a background tab is fine). They don't fire with the browser fully closed.
  On `http://localhost` they work after you click "allow".
- **Calendar** is a local month view (clickable days); Google Calendar sync is a
  planned follow-up.
- A **Reddit** widget was originally planned but removed — Reddit shut down
  unauthenticated access to its public JSON (HTTP 403).
- The standalone window opens a browser (Firefox by default) rather than an
  embedded webview, because the common Go webview binding needs `webkit2gtk-4.0`,
  which Ubuntu 24.04 replaced with `4.1`.
