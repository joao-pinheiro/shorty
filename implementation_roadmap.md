# Shorty — Implementation Roadmap

This roadmap breaks the specification into ordered implementation phases. Each phase builds on the previous and produces a testable increment. Spec section references (S1, S6.2, etc.) link to `specification.md`.

---

## Phase 1: Project Scaffolding

**Goal**: Buildable Go project with config loading and database initialization.

### Steps
1. Initialize Go module: `go mod init shorty` in `backend/`
2. Add dependencies: `modernc.org/sqlite`, `github.com/labstack/echo/v4`, `golang.org/x/time/rate`, `github.com/skip2/go-qrcode`, `github.com/joho/godotenv` (S18)
3. Implement `internal/config/config.go` — load `.env` file via godotenv, read all env vars with defaults, validate `API_KEY` is set (fatal if missing) (S11, S5)
4. Implement `internal/model/model.go` — define `Link`, `Click`, `Tag` domain structs with JSON tags (S3.1)
5. Write `migrations/001_init.sql` — full schema with all tables, indexes, schema_version (S3.3)
6. Implement `internal/store/store.go` — define `Store` interface with all method signatures
7. Implement `internal/store/sqlite.go`:
   - Open read and write connection pools with correct `SetMaxOpenConns` (S9.3)
   - Apply PRAGMAs on each connection (S3.5)
   - Migration runner: read `schema_version`, apply pending migrations in transactions (S3.4)
8. Implement `backend/cmd/shorty/main.go` — load config, open store, start Echo server on configured port
9. Create `Makefile` with `dev-backend` target (S17.1)

### Verify
- `make dev-backend` starts the server (with `API_KEY` set)
- SQLite file is created with all tables
- Server logs startup message

### Files
- `backend/go.mod`, `backend/go.sum`
- `backend/cmd/shorty/main.go`
- `backend/internal/config/config.go`
- `backend/internal/model/model.go`
- `backend/internal/store/store.go`
- `backend/internal/store/sqlite.go`
- `backend/migrations/001_init.sql`
- `Makefile`
- `.env.example`
- `.gitignore` (include `.env`, `*.db`, `bin/`)

---

## Phase 2: Core Packages

**Goal**: Short code generation and URL validation, independently testable.

### Steps
1. Implement `internal/shortcode/shortcode.go` — `Generate(length int) string` using `crypto/rand`, charset `[a-zA-Z0-9]`, collision retry with length escalation (S4)
2. Implement `internal/urlcheck/urlcheck.go` — `Validate(rawURL string) error` with parse, scheme, host (net.IP methods + localhost reject), length checks (S7)
3. Write unit tests for both packages

### Verify
- `go test ./internal/shortcode/... -race` — generation, charset, length, uniqueness
- `go test ./internal/urlcheck/... -race` — valid URLs, invalid schemes, private IPs, IPv6-mapped addresses, localhost, too-long URLs

### Files
- `backend/internal/shortcode/shortcode.go`
- `backend/internal/shortcode/shortcode_test.go`
- `backend/internal/urlcheck/urlcheck.go`
- `backend/internal/urlcheck/urlcheck_test.go`

---

## Phase 3: Middleware

**Goal**: Auth, rate limiting, logging, CORS, security headers, body limit, recovery.

### Steps
1. Implement `internal/handler/middleware.go`:
   - **Auth middleware**: extract `Authorization: Bearer` header, constant-time compare with config API key, skip for unauthenticated routes (S5)
   - **Rate limit middleware**: per-IP token bucket with `golang.org/x/time/rate`, IP extraction via Echo `IPExtractor` respecting `TRUSTED_PROXIES`, per-endpoint rate configs, stale limiter purge goroutine, rate limit response headers (S8.1)
   - **Security headers middleware**: `X-Content-Type-Options: nosniff` on all responses, `X-Frame-Options: DENY` on redirects (S8.5)
2. Configure Echo built-in middleware in `main.go`:
   - `middleware.Recover()` (S13)
   - `middleware.CORSWithConfig()` (S8.4)
   - `middleware.BodyLimit("1M")` (S8.2)
   - Request logger via `slog` (S12)
3. Register middleware on Echo instance

### Verify
- Request without `Authorization` header to `/api/v1/links` returns 401
- Request with wrong key returns 401
- Rapid requests trigger 429 with rate limit headers
- Health check (`/api/health`) works without auth (S6.13)

### Files
- `backend/internal/handler/middleware.go`
- Updated `backend/cmd/shorty/main.go`

---

## Phase 4: Link CRUD + Redirect

**Goal**: Core URL shortening flow — create, redirect, list, get, update, delete.

### Steps
1. Implement store methods in `sqlite.go`:
   - `CreateLink(ctx, url, code, expiresAt) (Link, error)`
   - `GetLinkByCode(ctx, code) (Link, error)` — used by redirect, uses read pool
   - `GetLinkByID(ctx, id) (Link, error)`
   - `ListLinks(ctx, params) ([]Link, total int, error)` — pagination, search, sort (whitelist validation), active filter (S6.4)
   - `UpdateLink(ctx, id, patch) (Link, error)` — sets `updated_at` explicitly (S6.6)
   - `DeactivateExpiredLink(ctx, id) error` — lazy is_active=0 with updated_at (S6.1)
   - `DeleteLink(ctx, id) error` — cascade (S6.7)
2. Implement `internal/handler/links.go`:
   - `POST /api/v1/links` — validate URL (urlcheck), validate custom code (regex, reserved words, collision), generate code (shortcode), validate expires_in (positive, max 365d), create in store (S6.2)
   - `GET /api/v1/links` — parse query params, call store.ListLinks (S6.4)
   - `GET /api/v1/links/:id` — return link, expired links returned normally (S6.5)
   - `PATCH /api/v1/links/:id` — validate mutable fields, update (S6.6)
   - `DELETE /api/v1/links/:id` — hard delete (S6.7)
3. Implement `internal/handler/redirect.go`:
   - `GET /:code` — lookup by code, check active/expired, 302 redirect, Cache-Control header (S6.1)
   - Note: click recording is deferred to Phase 5
4. Implement `internal/handler/health.go` — `GET /api/health` (S6.13)
5. Register all routes in `main.go`

### Verify
- Create a link via `curl -X POST`, verify 201 response with generated code
- Create with custom code, verify code is used
- Create with invalid URL, verify 400
- Visit short URL in browser, verify 302 redirect
- List links with pagination, search, sort, active filter
- Update link is_active, verify redirect returns 410
- Delete link, verify 204 and subsequent 404

### Files
- Updated `backend/internal/store/sqlite.go`
- `backend/internal/handler/links.go`
- `backend/internal/handler/redirect.go`
- `backend/internal/handler/health.go`
- Updated `backend/cmd/shorty/main.go`

---

## Phase 5: Async Click Recording

**Goal**: Redirects record clicks without blocking the response.

### Steps
1. Add click channel and batch writer to `main.go` or a dedicated `internal/clickrecorder/recorder.go`:
   - Buffered channel (capacity from `CLICK_BUFFER_SIZE`)
   - Non-blocking send in redirect handler (`select` with `default` — drop + log warning) (S9.2)
   - Background goroutine: batch insert every `CLICK_FLUSH_INTERVAL` seconds or 500 items
   - Batch transaction: INSERT clicks + UPDATE links SET click_count = click_count + N, updated_at (S9.2)
2. Implement store methods:
   - `BatchInsertClicks(ctx, []Click) error`
   - `IncrementClickCounts(ctx, map[linkID]count) error`
3. Wire click channel into redirect handler
4. Implement graceful shutdown: drain channel, flush remaining batch, close DB (S10)

### Verify
- Create link, visit short URL, verify click_count increments (may need to wait up to flush interval)
- Verify redirect response time is not affected by click recording
- Stop server with SIGINT, verify pending clicks are flushed (check DB after shutdown)

### Files
- `backend/internal/clickrecorder/recorder.go` (or inline in main.go)
- Updated `backend/internal/store/sqlite.go`
- Updated `backend/internal/handler/redirect.go`
- Updated `backend/cmd/shorty/main.go`

---

## Phase 6: Tags

**Goal**: Tag CRUD and link-tag associations.

### Steps
1. Implement store methods:
   - `CreateTag(ctx, name) (Tag, error)` — enforce 100 tag limit (S6.11)
   - `ListTags(ctx) ([]TagWithCount, error)` — include link_count via JOIN (S6.10)
   - `DeleteTag(ctx, id) error` — cascade to link_tags (S6.12)
   - `SetLinkTags(ctx, linkID, []tagName) error` — create missing tags, replace link_tags (S6.6)
   - `GetLinkTags(ctx, linkID) ([]string, error)`
   - `GetLinksTagsBatch(ctx, []linkID) (map[linkID][]string, error)` — for list endpoint efficiency
2. Implement `internal/handler/tags.go`:
   - `GET /api/v1/tags` (S6.10)
   - `POST /api/v1/tags` — validate name regex, enforce limit (S6.11)
   - `DELETE /api/v1/tags/:id` (S6.12)
3. Update link handlers:
   - Create link: validate tag names (fail entire request if any invalid), auto-create tags, associate (S6.2)
   - Update link: replace tags when `tags` field present (S6.6)
   - List links: include tags in response, support `tag` query filter (S6.4)
   - Get link: include tags in response
4. Register tag routes in `main.go`

### Verify
- Create tag, list tags, delete tag
- Create link with tags, verify tags in response
- Update link tags (full replacement)
- Filter links by tag
- Verify 100 tag limit
- Delete tag, verify link loses tag but link persists

### Files
- `backend/internal/handler/tags.go`
- Updated `backend/internal/store/sqlite.go`
- Updated `backend/internal/handler/links.go`

---

## Phase 7: Bulk Create, Analytics, QR

**Goal**: Remaining API endpoints.

### Steps
1. Implement `internal/handler/bulk.go`:
   - `POST /api/v1/links/bulk` — iterate URLs independently, reuse single-create validation logic, collect results with 0-based indexes, return total/succeeded/failed counts (S6.3)
2. Implement store methods:
   - `GetClicksByDay(ctx, linkID, since) ([]DayCount, error)`
   - `GetClicksByHour(ctx, linkID, since) ([]HourCount, error)`
   - `GetPeriodClickCount(ctx, linkID, since) (int, error)`
3. Implement `internal/handler/analytics.go`:
   - `GET /api/v1/links/:id/analytics` — validate period (whitelist: 24h/7d/30d/all), query appropriate buckets (S6.8)
4. Implement `internal/qr/qr.go` — generate PNG using `go-qrcode`, accept size param (min 128, max 1024, default 256) (S6.9)
5. Add QR handler to links.go or a separate file:
   - `GET /api/v1/links/:id/qr` — auth required, 404 if link not found
6. Wire public `/:code/qr` into routing middleware (Phase 9 will handle this, but the QR generation function is ready)

### Verify
- Bulk create 3 URLs (1 invalid), verify response with total/succeeded/failed and per-item results
- Create link, visit it several times, verify analytics returns clicks_by_day
- Verify 24h period returns clicks_by_hour
- Fetch QR code, verify valid PNG image

### Files
- `backend/internal/handler/bulk.go`
- `backend/internal/handler/analytics.go`
- `backend/internal/qr/qr.go`
- Updated `backend/internal/store/sqlite.go`
- Updated `backend/internal/handler/links.go` (QR endpoint)

---

## Phase 8: Data Retention

**Goal**: Background cleanup of old click data.

### Steps
1. Implement retention goroutine (in main.go or a dedicated package):
   - If `DATA_RETENTION_DAYS > 0`, start a goroutine
   - Calculate next midnight UTC, sleep until then, then run daily
   - `DELETE FROM clicks WHERE clicked_at < datetime('now', '-N days')` (S14)
   - Log number of deleted rows
2. Respect graceful shutdown — stop the retention goroutine cleanly

### Verify
- Set `DATA_RETENTION_DAYS=1`, create link, add clicks with old timestamps (manually via SQL or test helper), verify cleanup runs and deletes them
- Verify click_count is NOT decremented

### Files
- Updated `backend/cmd/shorty/main.go` (or `backend/internal/retention/retention.go`)
- Updated `backend/internal/store/sqlite.go`

---

## Phase 9: Frontend Scaffolding

**Goal**: React project with Vite, Tailwind, routing, API client, and API key handling.

### Steps
1. Initialize frontend project: `npm create vite@latest frontend -- --template react-ts`
2. Install dependencies: `tailwindcss`, `react-router-dom`, `date-fns`, `recharts` (S15.1)
3. Configure Tailwind CSS
4. Configure Vite proxy to backend in `vite.config.ts` (dev mode)
5. Implement `src/types.ts` — TypeScript types matching API response shapes (S6.2 response, S6.4 response, S6.8, S6.10, etc.)
6. Implement `src/api/client.ts` — typed fetch wrapper for all API endpoints, reads `VITE_API_URL`, attaches `Authorization: Bearer` from localStorage (S15.4)
7. Implement API key handling:
   - On 401 response, show key prompt modal
   - Store key in localStorage
   - Option to clear/change key (S15.3)
8. Set up React Router in `App.tsx` — `/` → Dashboard, `*` → NotFound (S15.2)
9. Add `dev-frontend` target to Makefile (S17.1)

### Verify
- `make dev-frontend` starts Vite dev server
- API client can reach backend (through Vite proxy)
- Entering wrong API key shows error, correct key is stored and used

### Files
- `frontend/` — full Vite project scaffold
- `frontend/src/types.ts`
- `frontend/src/api/client.ts`
- `frontend/src/App.tsx`
- `frontend/src/pages/NotFound.tsx`
- Updated `Makefile`

---

## Phase 10: Frontend — Core Dashboard

**Goal**: Shorten form, link table, copy button, search, pagination.

### Steps
1. Implement `ShortenForm.tsx` — URL input, optional custom code, expiration dropdown (Never, 1h, 1d, 7d, 30d, Custom), tag multi-select, submit calls `createLink` (S15.2)
2. Implement `CopyButton.tsx` — copies short URL to clipboard, shows feedback
3. Implement `LinkRow.tsx` — single row with truncated URL, created date, click count, tags, status badge, action buttons (Copy, QR, Analytics expand, Activate/Deactivate, Delete)
4. Implement `SearchBar.tsx` — search input, sort dropdown, active/all filter
5. Implement `LinkTable.tsx` — renders rows, pagination controls, calls `getLinks` with params
6. Implement `useLinks.ts` hook — manages link list state, pagination, search params, refetch
7. Assemble `Dashboard.tsx` — ShortenForm at top, result display, SearchBar, LinkTable

### Verify
- Create a link via the form, see it appear in the table
- Copy short URL to clipboard
- Search by URL or code
- Sort by different columns
- Paginate through results
- Deactivate/activate a link
- Delete a link

### Files
- `frontend/src/components/ShortenForm.tsx`
- `frontend/src/components/CopyButton.tsx`
- `frontend/src/components/LinkRow.tsx`
- `frontend/src/components/LinkTable.tsx`
- `frontend/src/components/SearchBar.tsx`
- `frontend/src/hooks/useLinks.ts`
- `frontend/src/pages/Dashboard.tsx`

---

## Phase 11: Frontend — Features

**Goal**: Analytics panel, bulk modal, QR modal, tag management.

### Steps
1. Implement `AnalyticsPanel.tsx` — inline expand under link row, period selector, Recharts bar chart for clicks-over-time, total click count (S15.2)
2. Implement `BulkShortenModal.tsx` — textarea for URLs (one per line), submit, show per-line results with success/error indicators (S15.2)
3. Implement `QRCodeModal.tsx` — display QR code image from API, download as PNG button (S15.2)
4. Implement `TagFilter.tsx` — dropdown to filter links by tag (S15.2)
5. Implement `TagManager.tsx` — list tags with link counts, create/delete tags (S15.2)
6. Implement `useTags.ts` hook — manages tag list state, create, delete
7. Wire all components into Dashboard

### Verify
- Expand analytics for a link, see bar chart with period selector
- Bulk shorten multiple URLs, see per-line results
- Open QR modal, download PNG
- Filter links by tag
- Create and delete tags in tag manager

### Files
- `frontend/src/components/AnalyticsPanel.tsx`
- `frontend/src/components/BulkShortenModal.tsx`
- `frontend/src/components/QRCodeModal.tsx`
- `frontend/src/components/TagFilter.tsx`
- `frontend/src/components/TagManager.tsx`
- `frontend/src/hooks/useTags.ts`
- Updated `frontend/src/pages/Dashboard.tsx`

---

## Phase 12: Production Serving + Routing Middleware

**Goal**: Single binary with embedded frontend and correct routing.

### Steps
1. Add `//go:embed` directive for `frontend/dist/` in a Go file (S15.5)
2. Implement the catch-all routing middleware in `middleware.go` or a dedicated file:
   - Check if path matches embedded static file → serve it
   - Check if path matches `/:code/qr` → generate and serve QR (no auth, no link status check) (S6.9)
   - Check if path matches a short code in DB → 302 redirect (S6.1)
   - Fallback → serve `index.html` (S15.5)
3. Apply rate limiting to `/:code` and `/:code/qr` within the middleware (S8.1)
4. Register the middleware after all `/api/*` routes
5. Update `Makefile` with `build-frontend`, `build-backend` (with `CGO_ENABLED=0`), and combined `build` target (S17.1)

### Verify
- `make build` produces a single binary in `bin/`
- Run the binary, visit `http://localhost:8080/` — frontend loads
- Static assets (JS, CSS) load correctly
- Short URL redirects still work
- Public `/:code/qr` returns QR code without auth
- Unknown paths serve `index.html` (SPA fallback)

### Files
- `backend/embed.go` (or in main.go)
- Updated `backend/internal/handler/middleware.go`
- Updated `backend/cmd/shorty/main.go`
- Updated `Makefile`

---

## Phase 13: Docker + Deployment

**Goal**: Containerized deployment.

### Steps
1. Write `Dockerfile` — multi-stage: Node build, Go build (CGO_ENABLED=0), distroless runtime (S17.2)
2. Write `docker-compose.yml` — single service, volume for `shorty.db`, environment variables (S17.3)
3. Write `.env.example` — all config vars with comments
4. Add `lint` target to Makefile (S17.1)

### Verify
- `docker compose up --build` starts the service
- Create a link, restart container, verify link persists (volume mount)
- Verify `.env` file is loaded when present

### Files
- `Dockerfile`
- `docker-compose.yml`
- `.env.example`
- Updated `Makefile`

---

## Phase 14: Testing

**Goal**: Meet 80% backend coverage target, cover all frontend flows.

### Steps
1. Backend unit tests:
   - `store`: all methods against `:memory:` SQLite — CRUD links, tags, clicks, pagination, search, tag filter, migration versioning
   - `handler`: each handler using `echo.NewContext` with mock store — all success paths, all error paths, auth rejection, rate limit headers
   - `shortcode`: already done in Phase 2
   - `urlcheck`: already done in Phase 2
   - `middleware`: auth bypass for public routes, rate limit enforcement, CORS headers, body limit
2. Backend integration tests:
   - Full server via `httptest.NewServer`
   - End-to-end: create link → redirect → verify click count → check analytics
   - Bulk create with mixed success/failure
   - Tag lifecycle: create tags → assign to link → filter by tag → delete tag
   - Expiration: create with short expires_in → wait → verify 410
   - Click buffer: verify clicks are recorded after flush interval
3. Frontend tests:
   - Set up Vitest + React Testing Library + MSW
   - ShortenForm: submit, validation errors, custom code
   - LinkTable: rendering, pagination, search, sort
   - CopyButton: clipboard interaction
   - AnalyticsPanel: period switching, chart rendering
   - BulkShortenModal: submit, per-line results
   - API key prompt on 401
4. Add `test-backend`, `test-frontend`, `test` targets to Makefile
5. Verify coverage: `go test ./... -race -coverprofile=coverage.out && go tool cover -func=coverage.out`

### Verify
- `make test` passes all backend and frontend tests
- Backend coverage ≥ 80%
- All user-facing flows covered in frontend tests

### Files
- `backend/internal/store/sqlite_test.go`
- `backend/internal/handler/*_test.go`
- `backend/internal/handler/middleware_test.go`
- `frontend/src/**/*.test.tsx`
- `frontend/src/test/setup.ts` (MSW setup)
- Updated `Makefile`

---

## Phase Summary

| Phase | Description | Depends On |
|-------|-------------|------------|
| 1 | Project scaffolding, config, DB | — |
| 2 | Short code + URL validation | 1 |
| 3 | Middleware (auth, rate limit, CORS, etc.) | 1 |
| 4 | Link CRUD + redirect | 1, 2, 3 |
| 5 | Async click recording | 4 |
| 6 | Tags | 4 |
| 7 | Bulk create, analytics, QR | 4, 5, 6 |
| 8 | Data retention | 5 |
| 9 | Frontend scaffolding | 4 (API available) |
| 10 | Frontend core dashboard | 9 |
| 11 | Frontend features | 10 |
| 12 | Production serving + routing | 7, 11 |
| 13 | Docker + deployment | 12 |
| 14 | Testing | All |
