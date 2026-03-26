# Phase 12: Production Serving + Routing Middleware

## Summary

Embeds the built frontend into the Go binary using `embed.FS` and implements a catch-all routing middleware that serves static assets, public QR codes, short code redirects, and SPA fallback — all without conflicting with Echo's `/api/*` routes. References: S15.5, S6.1, S6.9, S8.1, S17.1, S19.

**Depends on**: Phase 7 (all API endpoints including QR generation), Phase 11 (frontend build output).

---

## Files to Create/Modify

| File | Action |
|------|--------|
| `backend/embed.go` | Create |
| `backend/internal/handler/frontend.go` | Create |
| `backend/cmd/shorty/main.go` | Modify |
| `Makefile` | Modify |

---

## Step 1: Embed Frontend Assets

**File**: `backend/embed.go`

This file lives at the `backend/` package root so the embed directive can reference `frontend/dist/` relative to the module root. Alternatively, place it wherever the build layout allows — the key is the `//go:embed` path must resolve at compile time.

```go
package backend

import "embed"

// FrontendAssets holds the built frontend files.
// The embed path is relative to this file's directory.
// During build, frontend/dist/ must exist (built by npm run build).
//go:embed all:frontend/dist
var FrontendAssets embed.FS
```

**Important**: The `all:` prefix includes files starting with `.` or `_` which Vite may produce. The actual embed path depends on the directory relationship. If `backend/` is the Go module root and `frontend/` is a sibling, this file should live one level up at the repo root, or use a different arrangement.

### Recommended Approach

Place the embed file in `backend/cmd/shorty/` and copy/symlink `frontend/dist` during build, or restructure:

**File**: `backend/cmd/shorty/embed.go`

```go
package main

import "embed"

// FrontendDist holds the compiled frontend assets.
// Built by: cd frontend && npm run build
// The build step copies frontend/dist/ to backend/cmd/shorty/dist/ before go build.
//go:embed all:dist
var frontendDist embed.FS
```

The Makefile `build` target must ensure `backend/cmd/shorty/dist/` contains the frontend build output before `go build`.

### Alternative (embed from repo root)

If the Go module is at `backend/` and can't reference `../frontend/dist`, the cleanest approach:

1. Build frontend: `cd frontend && npm run build` → produces `frontend/dist/`.
2. Copy: `cp -r frontend/dist backend/cmd/shorty/dist`.
3. Go build embeds `backend/cmd/shorty/dist`.

---

## Step 2: Frontend File Server Setup

**File**: `backend/internal/handler/frontend.go`

```go
package handler

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// FrontendHandler serves embedded frontend static files and provides SPA fallback.
type FrontendHandler struct {
	// fileSystem is the embedded frontend/dist filesystem, sub-rooted to remove the "dist" prefix.
	fileSystem http.FileSystem
	// indexHTML is the pre-read contents of index.html for SPA fallback.
	indexHTML []byte
}

// NewFrontendHandler creates a handler from the embedded FS.
// distFS should be the embed.FS, subDir is the prefix to strip (e.g., "dist").
func NewFrontendHandler(embeddedFS fs.FS, subDir string) (*FrontendHandler, error) {
	subFS, err := fs.Sub(embeddedFS, subDir)
	if err != nil {
		return nil, fmt.Errorf("fs.Sub(%s): %w", subDir, err)
	}

	indexHTML, err := fs.ReadFile(subFS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read index.html: %w", err)
	}

	return &FrontendHandler{
		fileSystem: http.FS(subFS),
		indexHTML:  indexHTML,
	}, nil
}

// HasStaticFile checks if the given path corresponds to a file in the embedded filesystem.
// Returns true for JS, CSS, images, fonts, etc. Does NOT match index.html directly
// (that's served as SPA fallback).
func (fh *FrontendHandler) HasStaticFile(path string) bool {
	// Strip leading slash
	name := strings.TrimPrefix(path, "/")
	if name == "" || name == "index.html" {
		return false
	}

	f, err := fh.fileSystem.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return false
	}
	// Only serve files, not directories
	return !stat.IsDir()
}

// ServeStaticFile serves a static asset from the embedded filesystem.
func (fh *FrontendHandler) ServeStaticFile(c echo.Context) error {
	http.FileServer(fh.fileSystem).ServeHTTP(c.Response(), c.Request())
	return nil
}

// ServeIndex serves index.html for SPA client-side routing.
func (fh *FrontendHandler) ServeIndex(c echo.Context) error {
	return c.HTMLBlob(http.StatusOK, fh.indexHTML)
}
```

---

## Step 3: Catch-All Routing Middleware

**File**: `backend/internal/handler/frontend.go` (continued) or `backend/internal/handler/catchall.go`

This is the core routing logic from S15.5. It's registered as an Echo middleware **after** all `/api/*` routes so those take priority.

### RateLimiter Interface

```go
// RateLimiter wraps the per-IP token bucket from Phase 3's rate limit middleware.
// Allow(ip) checks if the request should be permitted under the redirect rate limit
// (100/min, burst 200).
type RateLimiter interface {
	Allow(ip string) bool
}
```

### Function Signature

```go
// CatchAllMiddleware returns an Echo middleware that handles all non-API requests.
// It checks in order:
//   1. Static file in embedded frontend → serve it
//   2. Path matches /:code/qr → serve public QR code (no auth)
//   3. Path matches a short code in DB → 302 redirect
//   4. Fallback → serve index.html (SPA routing)
func CatchAllMiddleware(
	frontend *FrontendHandler,
	store store.Store,
	qrGenerator func(url string, size int) ([]byte, error),
	baseURL string,
	redirectRateLimiter RateLimiter,  // rate limiter for redirect + public QR (S8.1)
) echo.MiddlewareFunc
```

### Implementation

```go
func CatchAllMiddleware(
	frontend *FrontendHandler,
	store store.Store,
	qrGen func(url string, size int) ([]byte, error),
	baseURL string,
	rl RateLimiter,
) echo.MiddlewareFunc {
	// Regex to match /:code/qr pattern
	qrPathRegex := regexp.MustCompile(`^/([a-zA-Z0-9_-]{3,32})/qr$`)

	// Short code regex for single segment paths
	codeRegex := regexp.MustCompile(`^/([a-zA-Z0-9_-]{3,32})$`)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path

			// Skip /api/* paths — let Echo's registered routes handle them
			if strings.HasPrefix(path, "/api/") || path == "/api" {
				return next(c)
			}

			// Set security headers INSIDE the catch-all middleware before any return,
			// rather than relying on a separate middleware (ensures headers are set
			// regardless of middleware ordering).
			c.Response().Header().Set("X-Content-Type-Options", "nosniff")

			// Normalize: strip trailing slash (S19)
			if len(path) > 1 && strings.HasSuffix(path, "/") {
				path = strings.TrimRight(path, "/")
			}

			// 1. Check for static file in embedded frontend
			if frontend.HasStaticFile(path) {
				return frontend.ServeStaticFile(c)
			}

			// 2. Check /:code/qr pattern (S6.9 — public, no auth)
			if matches := qrPathRegex.FindStringSubmatch(path); matches != nil {
				code := matches[1]

				// Rate limit (S8.1: redirect + public QR: 100/min, burst 200)
				if !rl.Allow(c.RealIP()) {
					return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
				}

				// Look up link (no status check — QR always returned per S6.9)
				link, err := store.GetLinkByCode(c.Request().Context(), code)
				if err != nil {
					// Not found → 404, NOT SPA fallback (path matched short code pattern)
					return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
				}

				// Parse optional size param
				size := 256
				if sizeStr := c.QueryParam("size"); sizeStr != "" {
					parsed, err := strconv.Atoi(sizeStr)
					if err == nil && parsed >= 128 && parsed <= 1024 {
						size = parsed
					}
				}

				shortURL := baseURL + "/" + link.Code
				png, err := qrGen(shortURL, size)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate QR code")
				}

				return c.Blob(http.StatusOK, "image/png", png)
			}

			// 3. Check if path matches a short code → redirect (S6.1)
			// If the code is NOT found in DB, fall through to SPA fallback (step 4)
			// because we can't distinguish a missing short code from a SPA route
			// like /dashboard or /settings.
			if matches := codeRegex.FindStringSubmatch(path); matches != nil {
				code := matches[1]

				link, err := store.GetLinkByCode(c.Request().Context(), code)
				if err != nil {
					// Not a known short code → fall through to SPA fallback
					return frontend.ServeIndex(c)
				}

				// Rate limit (S8.1: 100/min, burst 200) — only for actual redirects
				if !rl.Allow(c.RealIP()) {
					return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
				}

				// Check active/expired status (S6.1)
				if !link.IsActive {
					return c.JSON(http.StatusGone, map[string]string{
						"error": "link is deactivated",
					})
				}

				if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now()) {
					// Lazy deactivation (S6.1, S6.6)
					_ = store.DeactivateExpiredLink(c.Request().Context(), link.ID)
					return c.JSON(http.StatusGone, map[string]string{
						"error": "link has expired",
					})
				}

				// Non-blocking send to click channel
				select {
				case clickChan <- store.ClickEvent{LinkID: link.ID, ClickedAt: time.Now()}:
				default:
					slog.Warn("click buffer full, dropping click")
				}

				// Security headers for redirect (S8.5)
				c.Response().Header().Set("X-Frame-Options", "DENY")
				c.Response().Header().Set("Cache-Control", "private, max-age=0")

				return c.Redirect(http.StatusFound, link.OriginalURL)
			}

			// 4. Fallback: path doesn't match short code pattern (e.g., /dashboard, /settings)
			// → serve index.html for SPA routing (S15.5)
			return frontend.ServeIndex(c)
		}
	}
}
```

### Key Design Decisions

- **`/api/*` is skipped entirely** — Echo routes handle those. The middleware calls `next(c)` for API paths.
- **Static file check is first** so that `assets/index-abc123.js` is served directly without hitting the DB.
- **QR pattern is checked before generic code pattern** because `/:code/qr` would also match `codeRegex` if "qr" were a valid code.
- **Bare `/:code` paths fall through to SPA when code is not found in DB** because we can't distinguish a missing short code from a SPA route like `/dashboard` or `/settings`. Only `/:code/qr` returns `404 {"error": "not found"}` for unknown codes since that path is unambiguously a QR request, not a SPA route.
- **Rate limiting** is applied to redirect and public QR but NOT to static file serving or SPA fallback.

---

## Step 4: Click Recording Integration

The catch-all middleware needs access to the click recording channel from Phase 5. Two approaches:

### Option A: Pass channel into middleware

```go
func CatchAllMiddleware(
	// ... existing params
	clickChan chan<- ClickEvent,
) echo.MiddlewareFunc {
	// Inside the redirect branch:
	select {
	case clickChan <- ClickEvent{LinkID: link.ID, ClickedAt: time.Now()}:
	default:
		// Channel full — drop click, log warning (S9.2)
		slog.Warn("click buffer full, dropping click", "link_id", link.ID)
	}
}
```

### Option B: Accept a ClickRecorder interface

```go
type ClickRecorder interface {
	Record(linkID int64)
}
```

Option B is cleaner for testing. The concrete implementation wraps the buffered channel from Phase 5.

---

## Step 5: Register Middleware in main.go

**File**: `backend/cmd/shorty/main.go` (modify)

### Registration Order

```go
func main() {
	// ... config, store, etc.

	e := echo.New()

	// Global middleware
	e.Use(middleware.Recover())
	e.Use(corsMiddleware(cfg))
	e.Use(middleware.BodyLimit("1M"))
	e.Use(requestLoggerMiddleware())
	e.Use(securityHeadersMiddleware())

	// API routes (registered FIRST — they take priority)
	api := e.Group("/api")
	api.GET("/health", healthHandler)

	v1 := api.Group("/v1", authMiddleware(cfg.APIKey))
	v1.POST("/links", createLink)
	v1.POST("/links/bulk", bulkCreate)
	v1.GET("/links", listLinks)
	v1.GET("/links/:id", getLink)
	v1.PATCH("/links/:id", updateLink)
	v1.DELETE("/links/:id", deleteLink)
	v1.GET("/links/:id/analytics", getAnalytics)
	v1.GET("/links/:id/qr", getQR)
	v1.GET("/tags", listTags)
	v1.POST("/tags", createTag)
	v1.DELETE("/tags/:id", deleteTag)

	// Catch-all middleware (registered AFTER API routes)
	frontendHandler, err := handler.NewFrontendHandler(frontendDist, "dist")
	if err != nil {
		slog.Error("failed to initialize frontend handler", "error", err)
		os.Exit(1)
	}

	redirectRL := newRedirectRateLimiter(cfg)  // 100/min, burst 200

	e.Use(handler.CatchAllMiddleware(
		frontendHandler,
		store,
		qr.Generate,
		cfg.BaseURL,
		redirectRL,
		clickRecorder,
	))

	// Start server...
}
```

### Why Middleware Instead of Wildcard Routes

Per S15.5: "This avoids Echo wildcard route conflicts." Echo does not allow both `/:code` and `/api/*` as registered routes — they conflict. The catch-all middleware sidesteps this by intercepting requests before Echo's router for non-API paths.

---

## Step 6: Update Makefile

**File**: `Makefile` (modify — add/update targets)

```makefile
.PHONY: dev-backend dev-frontend build-backend build-frontend build test-backend test-frontend test lint migrate

# Development
dev-backend:
	cd backend && go run ./cmd/shorty

dev-frontend:
	cd frontend && npm run dev

# Build frontend
build-frontend:
	cd frontend && npm ci && npm run build

# Copy frontend dist into Go embed location
copy-frontend: build-frontend
	rm -rf backend/cmd/shorty/dist
	cp -r frontend/dist backend/cmd/shorty/dist

# Build backend (requires frontend to be built and copied first)
build-backend: copy-frontend
	cd backend && CGO_ENABLED=0 go build -o ../bin/shorty ./cmd/shorty

# Combined build
build: build-backend

# Testing
test-backend:
	cd backend && go test ./... -race

test-frontend:
	cd frontend && npm test

test: test-backend test-frontend

# Linting
lint:
	cd backend && golangci-lint run ./...
	cd frontend && npm run lint

# Migration
migrate:
	cd backend && go run ./cmd/shorty -migrate
```

### Key Points

- `build-frontend` runs `npm ci` (deterministic install) then `npm run build`.
- `copy-frontend` copies `frontend/dist/` to `backend/cmd/shorty/dist/` so the `//go:embed` directive can find it.
- `build-backend` sets `CGO_ENABLED=0` for static binary (pure Go SQLite driver, S17.2).
- `build` is the single command to produce the final binary at `bin/shorty`.
- The `rm -rf` before `cp -r` ensures stale files are cleaned.

---

## Step 7: Vite Build Configuration

**File**: `frontend/vite.config.ts` (verify/modify)

Ensure production build outputs to `frontend/dist/` with hashed filenames for cache busting:

```typescript
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'dist',
    sourcemap: false,
    rollupOptions: {
      output: {
        // Hashed filenames for long-term caching
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash].[ext]',
      },
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});
```

In production (served by Go), the Vite proxy is not used — all requests go to the Go server, and the catch-all middleware handles routing.

---

## Verification Checklist

1. **Full build**:
   - `make build` succeeds, producing `bin/shorty`.
   - Binary size is reasonable (single static binary).

2. **Frontend serving**:
   - Run `API_KEY=test ./bin/shorty`.
   - Visit `http://localhost:8080/` — React app loads.
   - Check browser Network tab: JS/CSS assets load with hashed filenames.
   - No CORS errors (same origin).

3. **Static assets**:
   - `/assets/index-abc123.js` serves the JavaScript bundle.
   - Content-Type headers are correct (JS, CSS, images).

4. **Short code redirect**:
   - Create a link via API, visit `http://localhost:8080/{code}` — 302 redirect works.
   - Check `Cache-Control: private, max-age=0` header.
   - Check `X-Frame-Options: DENY` header.

5. **Public QR**:
   - Visit `http://localhost:8080/{code}/qr` — returns PNG without auth.
   - Visit `http://localhost:8080/{code}/qr?size=512` — larger image.
   - Invalid size values ignored (uses default 256).

6. **SPA fallback**:
   - Visit `http://localhost:8080/dashboard` — serves index.html, React app renders.
   - Visit `http://localhost:8080/abc123` where `abc123` is not a real code — serves index.html (SPA fallback), because bare `/:code` paths fall through when code not found in DB.
   - Visit `http://localhost:8080/abc123/qr` where `abc123` is not a real code — returns `404 {"error": "not found"}` (QR path is unambiguously not a SPA route).

7. **Trailing slash normalization** (S19):
   - `http://localhost:8080/{code}/` redirects same as `http://localhost:8080/{code}`.

8. **Rate limiting**:
   - Rapid requests to `/{code}` eventually return 429.
   - Rate limit headers present: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`.
