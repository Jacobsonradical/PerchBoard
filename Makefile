# PerchBoard build helpers.
# The frontend (web/) is built first; its output (web/dist) is embedded into the
# Go binary, so `make build` produces one self-contained executable.

BIN := perchboard

.PHONY: build web run dev docker clean

## build: build the frontend then the single Go binary (default run = standalone window)
build: web
	go build -o $(BIN) ./

## web: install deps (first time) and build the React frontend into web/dist
web:
	cd web && npm install && npm run build

## run: build everything and launch (opens a standalone app-mode window, or browser)
run: build
	./$(BIN)

## dev: run backend + Vite dev server with hot reload (two terminals recommended)
##   terminal 1: make dev-api    terminal 2: make dev-web
dev-api:
	go run ./ --serve
dev-web:
	cd web && npm run dev

## docker: build the container image (serves the web app on :7171)
docker:
	docker build -t perchboard:latest .

clean:
	rm -f $(BIN)
	rm -rf web/dist web/node_modules
