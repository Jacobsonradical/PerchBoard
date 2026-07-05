# Multi-stage build: build the frontend, embed it into the Go binary, then ship
# a tiny runtime image. This is the "anyone can run it" path (web app on :7171).
#
# For multi-arch (amd64 + arm64) the heavy stages run on the builder's NATIVE
# architecture and cross-compile, instead of emulating the whole toolchain under
# QEMU — the frontend is arch-independent JS, and the Go binary cross-compiles
# cheaply (CGO disabled). Only the small runtime stage is the target arch.

# 1) Build the React frontend (same output for every target arch).
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /app/web
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# 2) Cross-compile the Go binary for the target arch on the native builder.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS gobuild
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /perchboard ./

# 3) Minimal runtime (target arch). Chromium is included because the ScholarOne
# widget drives a headless browser to log in and read the journal dashboards;
# the rest of the app works without it.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates chromium nss freetype harfbuzz ttf-freefont
# Let chromedp find the browser inside the container.
ENV CHROME_BIN=/usr/bin/chromium-browser
COPY --from=gobuild /perchboard /usr/local/bin/perchboard
# Persist dashboard config + uploaded backgrounds here (mount a volume).
ENV PERCHBOARD_DATA=/data
VOLUME /data
EXPOSE 7171
# Headless container: serve only, bound to all interfaces.
ENTRYPOINT ["perchboard", "--serve", "--addr", "0.0.0.0:7171"]
