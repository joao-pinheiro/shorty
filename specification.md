# Shorty — URL Shortener Service Specification

## 1. Overview

Shorty is a self-hosted URL shortener service with a Go REST API backend, React SPA frontend, and SQLite database. It is deployed as a single binary with the frontend embedded.

### Tech Stack
- **Backend**: Go 1.22+ (stdlib HTTP router)
- **Frontend**: React 18+ / TypeScript / Vite / Tailwind CSS
- **Database**: SQLite (via `github.com/mattn/go-sqlite3`)
- **Deployment**: Single binary with embedded frontend assets

---

## 2. Project Structure

```
shorty/
├── backend/
│   ├── cmd/
│   │   └── shorty/
│   │       └── main.go                # Entrypoint, wiring, graceful shutdown
│   ├── internal/
│   │   ├── config/
│   │   │   └── config.go              # Env var loading with defaults
│   │   ├── handler/
│   │   │   ├── redirect.go            # GET /:code
│   │   │   ├── links.go               # CRUD for links
│   │   │   ├── analytics.go           # Click analytics
│   │   │   ├── bulk.go                # Bulk link creation
│   │   │   ├── tags.go                # Tag CRUD
│   │   │   ├── health.go              # Health check
│   │   │   └── middleware.go           # Auth, rate limit, logging, CORS, recovery
│   │   ├── model/
│   │   │   └── model.go               # Domain structs
│   │   ├── store/
│   │   │   ├── store.go               # Store interface
│   │   │   └── sqlite.go              # SQLite implementation
│   │   ├── shortcode/
│   │   │   └── shortcode.go           # Code generation, collision retry
│   │   ├── urlcheck/
│   │   │   └── urlcheck.go            # URL validation, SSRF prevention
│   │   └── qr/
│   │       └── qr.go                  # QR code PNG generation
│   ├── migrations/
│   │   └── 001_init.sql
│   ├── go.mod
│   └── go.sum
├── frontend/
│   ├── public/
│   ├── src/
│   │   ├── api/
│   │   │   └── client.ts              # Typed API client
│   │   ├── components/
│   │   │   ├── ShortenForm.tsx
│   │   │   ├── LinkTable.tsx
│   │   │   ├── LinkRow.tsx
│   │   │   ├── AnalyticsPanel.tsx
│   │   │   ├── BulkShortenModal.tsx
│   │   │   ├── QRCodeModal.tsx
│   │   │   ├── SearchBar.tsx
│   │   │   ├── TagFilter.tsx
│   │   │   ├── TagManager.tsx
│   │   │   └── CopyButton.tsx
│   │   ├── pages/
│   │   │   ├── Dashboard.tsx
│   │   │   └── NotFound.tsx
│   │   ├── hooks/
│   │   │   ├── useLinks.ts
│   │   │   └── useTags.ts
│   │   ├── App.tsx
│   │   ├── index.tsx
│   │   └── types.ts
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── Makefile
├── Dockerfile
└── docker-compose.yml
```

---

## 3. Database Schema

SQLite file: `shorty.db`

### 3.1 Tables

#### links

| Column       | Type     | Constraints                        |
|--------------|----------|------------------------------------|
| id           | INTEGER  | PRIMARY KEY AUTOINCREMENT          |
| code         | TEXT     | UNIQUE NOT NULL                    |
| original_url | TEXT     | NOT NULL                           |
| created_at   | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP |
| expires_at   | DATETIME | NULL (NULL = never expires)        |
| is_active    | INTEGER  | NOT NULL DEFAULT 1                 |
| click_count  | INTEGER  | NOT NULL DEFAULT 0                 |

#### clicks

| Column     | Type     | Constraints                        |
|------------|----------|------------------------------------|
| id         | INTEGER  | PRIMARY KEY AUTOINCREMENT          |
| link_id    | INTEGER  | NOT NULL, FK → links(id) ON DELETE CASCADE |
| clicked_at | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP |

#### tags

| Column     | Type     | Constraints                        |
|------------|----------|------------------------------------|
| id         | INTEGER  | PRIMARY KEY AUTOINCREMENT          |
| name       | TEXT     | UNIQUE NOT NULL                    |
| created_at | DATETIME | NOT NULL DEFAULT CURRENT_TIMESTAMP |

#### link_tags

| Column  | Type    | Constraints                              |
|---------|---------|------------------------------------------|
| link_id | INTEGER | NOT NULL, FK → links(id) ON DELETE CASCADE |
| tag_id  | INTEGER | NOT NULL, FK → tags(id) ON DELETE CASCADE  |

PRIMARY KEY (link_id, tag_id)

### 3.2 Indexes

- `idx_links_code` UNIQUE on `links(code)`
- `idx_links_created_at` on `links(created_at)`
- `idx_links_expires_at` on `links(expires_at)` WHERE `expires_at IS NOT NULL`
- `idx_clicks_link_id` on `clicks(link_id)`
- `idx_clicks_clicked_at` on `clicks(clicked_at)`
- `idx_link_tags_tag_id` on `link_tags(tag_id)`

### 3.3 Migration SQL (001_init.sql)

```sql
CREATE TABLE IF NOT EXISTS links (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    code         TEXT    UNIQUE NOT NULL,
    original_url TEXT    NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   DATETIME,
    is_active    INTEGER NOT NULL DEFAULT 1,
    click_count  INTEGER NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_links_code ON links(code);
CREATE INDEX IF NOT EXISTS idx_links_created_at ON links(created_at);
CREATE INDEX IF NOT EXISTS idx_links_expires_at ON links(expires_at) WHERE expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS clicks (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    link_id    INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    clicked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_clicks_link_id ON clicks(link_id);
CREATE INDEX IF NOT EXISTS idx_clicks_clicked_at ON clicks(clicked_at);

CREATE TABLE IF NOT EXISTS tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT UNIQUE NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS link_tags (
    link_id INTEGER NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (link_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_link_tags_tag_id ON link_tags(tag_id);
```

### 3.4 SQLite PRAGMAs (set at connection open)

```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA cache_size=-64000;       -- 64MB page cache
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
PRAGMA temp_store=MEMORY;
```

---

## 4. Short Code Generation

### Algorithm
1. Generate cryptographically random 6-character code from charset `[a-zA-Z0-9]` (62 chars).
2. Check for collision: `SELECT 1 FROM links WHERE code = ?`.
3. On collision, retry up to 3 times.
4. If all retries collide, increase length to 7 and retry 3 more times.
5. If still colliding, return 500 Internal Server Error.

### Capacity
- 62^6 = ~56.8 billion possible codes
- 62^7 = ~3.5 trillion possible codes

### Custom Aliases
- Must match regex: `^[a-zA-Z0-9_-]{3,32}$`
- Reserved words (rejected): `api`, `health`, `admin`, `static`, `assets`, `favicon.ico`, `robots.txt`, `.well-known`
- Collision with existing code returns 409 Conflict

---

## 5. Authentication

### Mechanism
- Single API key configured via `API_KEY` environment variable
- Passed in HTTP header: `Authorization: Bearer <key>`
- If `API_KEY` is empty/unset, auth is disabled (all endpoints open)

### Scope
- **Required**: All `/api/v1/*` endpoints (reads and writes)
- **Not required**: `GET /:code` (redirect), `GET /api/health`

### Error Responses
- Missing header: `401 {"error": "missing authorization header"}`
- Invalid key: `401 {"error": "invalid API key"}`

---

## 6. REST API

Base path: `/api/v1`
Redirect handler: root `GET /:code`

### 6.1 Redirect

```
GET /:code
```

**Auth**: No

**Behavior**:
- Lookup `code` in `links` table
- Not found → `404 {"error": "not found"}`
- Found but `is_active = 0` → `410 {"error": "link is deactivated"}`
- Found but `expires_at < NOW()` → `410 {"error": "link has expired"}` (lazily set `is_active = 0`)
- Valid → `301` redirect with `Location` header
- `Cache-Control: private, max-age=0` (preserve analytics accuracy)
- Click recorded asynchronously via buffered channel

### 6.2 Create Link

```
POST /api/v1/links
Content-Type: application/json
Authorization: Bearer <key>
```

**Request**:
```json
{
  "url": "https://example.com/very-long-path",
  "custom_code": "my-link",
  "expires_in": 86400,
  "tags": ["marketing", "campaign-q1"]
}
```

All fields except `url` are optional. `expires_in` is seconds from now. `tags` are created if they don't exist.

**Response 201**:
```json
{
  "id": 1,
  "code": "aB3xYz",
  "short_url": "http://localhost:8080/aB3xYz",
  "original_url": "https://example.com/very-long-path",
  "created_at": "2026-03-26T10:00:00Z",
  "expires_at": "2026-03-27T10:00:00Z",
  "is_active": true,
  "click_count": 0,
  "tags": ["marketing", "campaign-q1"]
}
```

**Errors**:

| Condition | Status | Body |
|-----------|--------|------|
| Invalid/missing URL | 400 | `{"error": "invalid URL: must be http or https"}` |
| URL too long (>2048 chars) | 400 | `{"error": "URL exceeds 2048 characters"}` |
| Custom code already taken | 409 | `{"error": "code already in use"}` |
| Custom code invalid format | 400 | `{"error": "code must be 3-32 alphanumeric, dash, or underscore"}` |
| Custom code reserved | 400 | `{"error": "code is reserved"}` |
| Malicious URL detected | 400 | `{"error": "URL flagged as potentially unsafe"}` |
| Rate limited | 429 | `{"error": "rate limit exceeded", "retry_after": 60}` |

### 6.3 Bulk Create

```
POST /api/v1/links/bulk
Content-Type: application/json
Authorization: Bearer <key>
```

**Request**:
```json
{
  "urls": [
    {"url": "https://example.com/1"},
    {"url": "https://example.com/2", "custom_code": "ex2"},
    {"url": "https://example.com/3", "expires_in": 3600, "tags": ["batch"]}
  ]
}
```

- Maximum 50 URLs per request.
- Processed in a single database transaction.
- Response preserves input order.

**Response 200**:
```json
{
  "results": [
    {"ok": true, "link": {"id": 1, "code": "aB3xYz", "...": "..."}},
    {"ok": true, "link": {"id": 2, "code": "ex2", "...": "..."}},
    {"ok": false, "error": "invalid URL: must be http or https", "index": 2}
  ]
}
```

### 6.4 List Links

```
GET /api/v1/links?page=1&per_page=20&search=example&sort=created_at&order=desc&active=true&tag=marketing
Authorization: Bearer <key>
```

**Query Parameters**:

| Param    | Default      | Description |
|----------|-------------|-------------|
| page     | 1           | Page number (1-indexed) |
| per_page | 20          | Items per page, max 100 |
| search   | (none)      | Substring match on `original_url` or `code` |
| sort     | `created_at` | One of: `created_at`, `click_count`, `expires_at` |
| order    | `desc`      | `asc` or `desc` |
| active   | (none)      | Filter: `true`, `false`, or omit for all |
| tag      | (none)      | Filter by tag name |

**Response 200**:
```json
{
  "links": [
    {
      "id": 1,
      "code": "aB3xYz",
      "short_url": "http://localhost:8080/aB3xYz",
      "original_url": "https://example.com/very-long-path",
      "created_at": "2026-03-26T10:00:00Z",
      "expires_at": null,
      "is_active": true,
      "click_count": 42,
      "tags": ["marketing"]
    }
  ],
  "total": 142,
  "page": 1,
  "per_page": 20
}
```

### 6.5 Get Single Link

```
GET /api/v1/links/:id
Authorization: Bearer <key>
```

**Response 200**: Same link object as above. **404** if not found.

### 6.6 Update Link

```
PATCH /api/v1/links/:id
Content-Type: application/json
Authorization: Bearer <key>
```

**Request**:
```json
{
  "is_active": false,
  "expires_at": "2026-04-01T00:00:00Z",
  "tags": ["updated-tag"]
}
```

Only `is_active`, `expires_at`, and `tags` are mutable. `code` and `original_url` are immutable after creation.

When `tags` is provided, it replaces all existing tags for the link (full replacement, not merge).

**Response 200**: Updated link object. **404** if not found.

### 6.7 Delete Link

```
DELETE /api/v1/links/:id
Authorization: Bearer <key>
```

Hard delete. Cascades to clicks and link_tags. **Response 204** No Content. **404** if not found.

### 6.8 Get Analytics

```
GET /api/v1/links/:id/analytics?period=7d
Authorization: Bearer <key>
```

**Period values**: `24h`, `7d`, `30d`, `all`

**Response 200**:
```json
{
  "link_id": 1,
  "total_clicks": 1523,
  "period_clicks": 342,
  "clicks_by_day": [
    {"date": "2026-03-25", "count": 45},
    {"date": "2026-03-26", "count": 52}
  ]
}
```

For `24h` period, `clicks_by_day` returns hourly buckets instead:
```json
{
  "clicks_by_hour": [
    {"hour": "2026-03-26T14:00:00Z", "count": 12},
    {"hour": "2026-03-26T15:00:00Z", "count": 8}
  ]
}
```

### 6.9 QR Code

```
GET /api/v1/links/:id/qr?size=256
Authorization: Bearer <key>
```

- Returns `image/png`
- `size` query param: pixel dimension (square), default 256, min 128, max 1024
- Generated on the fly
- **404** if link not found

### 6.10 List Tags

```
GET /api/v1/tags
Authorization: Bearer <key>
```

**Response 200**:
```json
{
  "tags": [
    {"id": 1, "name": "marketing", "created_at": "2026-03-26T10:00:00Z", "link_count": 15},
    {"id": 2, "name": "social", "created_at": "2026-03-26T11:00:00Z", "link_count": 8}
  ]
}
```

### 6.11 Create Tag

```
POST /api/v1/tags
Content-Type: application/json
Authorization: Bearer <key>
```

**Request**:
```json
{
  "name": "marketing"
}
```

Tag name must match `^[a-zA-Z0-9_-]{1,50}$`. **Response 201**. **409** if name already exists.

### 6.12 Delete Tag

```
DELETE /api/v1/tags/:id
Authorization: Bearer <key>
```

Cascades to link_tags (removes tag from all links). **Response 204**. **404** if not found.

### 6.13 Health Check

```
GET /api/health
```

**Auth**: No

**Response 200**:
```json
{"status": "ok", "version": "1.0.0"}
```

---

## 7. URL Validation

Multi-layer validation in `urlcheck` package:

1. **Parse**: `url.Parse()` must succeed
2. **Scheme**: Must be `http` or `https`. Reject `javascript:`, `data:`, `ftp:`, etc.
3. **Host**: Must have a non-empty host. Reject:
   - `localhost`, `127.0.0.0/8`
   - `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`
   - `::1`, `0.0.0.0`
4. **Length**: Max 2048 characters
5. **Safe Browsing** (optional): If `GOOGLE_SAFE_BROWSING_API_KEY` is set, check against Google Safe Browsing API v4. If not configured, skip gracefully.

---

## 8. Security

### 8.1 Rate Limiting

Token bucket per IP using `golang.org/x/time/rate`. Implemented in middleware with `sync.Map`.

| Endpoint | Rate | Burst |
|----------|------|-------|
| `POST /api/v1/links` | 10/min | 20 |
| `POST /api/v1/links/bulk` | 2/min | 5 |
| `GET /:code` (redirect) | 100/min | 200 |
| All other API endpoints | 30/min | 60 |

Stale limiters (no activity for 10 min) purged by background goroutine every 5 minutes.

Response headers on every request:
- `X-RateLimit-Limit`
- `X-RateLimit-Remaining`
- `X-RateLimit-Reset` (Unix timestamp)

### 8.2 Input Sanitization

- All user input trimmed of whitespace
- URLs: strip trailing whitespace, normalize scheme to lowercase
- Custom codes validated against regex
- SQL injection prevented by parameterized queries only (no string interpolation)
- All API responses have `Content-Type: application/json`

### 8.3 CORS

Configurable via `CORS_ALLOWED_ORIGINS` env var (comma-separated). Defaults to `http://localhost:5173`.

Headers:
- `Access-Control-Allow-Origin`
- `Access-Control-Allow-Methods: GET, POST, PATCH, DELETE, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type, Authorization`
- `Access-Control-Max-Age: 86400`

### 8.4 Security Headers

All responses:
- `X-Content-Type-Options: nosniff`

Redirect responses additionally:
- `X-Frame-Options: DENY` (prevent clickjacking via framed short URLs)

---

## 9. Performance

### 9.1 Redirect Latency Target

< 10ms p99

### 9.2 Async Click Recording

Redirect handler sends click data to a buffered channel (capacity configurable, default 10,000). A background goroutine batch-inserts clicks every 1 second or when the buffer reaches 500 items, whichever comes first. `click_count` on the `links` table is incremented in the same batch transaction.

### 9.3 Connection Management

- **Write pool**: Single `*sql.DB` with `SetMaxOpenConns(1)` (SQLite single-writer limitation)
- **Read pool**: Separate `*sql.DB` opened with `?mode=ro`, `SetMaxOpenConns(4)`
- Redirect lookups use the read pool
- Mutations use the write connection

### 9.4 API Response Target

< 50ms p95 for list/search operations.

- Pagination is mandatory (no unbounded queries)
- Search uses `LIKE '%term%'` (adequate for < 1M rows)

---

## 10. Configuration

All via environment variables with sensible defaults.

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `BASE_URL` | `http://localhost:8080` | Used to construct short URLs in responses |
| `DB_PATH` | `./shorty.db` | SQLite file path |
| `API_KEY` | (empty) | API key for auth. Empty = auth disabled |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173` | Comma-separated allowed origins |
| `DEFAULT_CODE_LENGTH` | `6` | Generated short code length |
| `MAX_BULK_URLS` | `50` | Max URLs in bulk endpoint |
| `CLICK_BUFFER_SIZE` | `10000` | Async click channel buffer capacity |
| `CLICK_FLUSH_INTERVAL` | `1s` | Batch insert interval for clicks |
| `RATE_LIMIT_ENABLED` | `true` | Toggle rate limiting on/off |
| `GOOGLE_SAFE_BROWSING_API_KEY` | (empty) | Enable safe browsing URL checks |
| `DATA_RETENTION_DAYS` | `0` | Days to keep click data (0 = forever) |

---

## 11. Logging

Structured JSON logging via Go stdlib `log/slog`.

Request logging middleware emits per request:
```json
{
  "level": "info",
  "msg": "request",
  "method": "GET",
  "path": "/aB3xYz",
  "status": 301,
  "duration_ms": 2.3,
  "ip": "192.168.1.1"
}
```

---

## 12. Error Handling

All errors return consistent JSON:

```json
{
  "error": "human-readable message"
}
```

| Status | Meaning |
|--------|---------|
| 400 | Validation error |
| 401 | Missing or invalid API key |
| 404 | Resource not found |
| 409 | Conflict (duplicate code or tag) |
| 410 | Gone (expired or deactivated link) |
| 429 | Rate limited (includes `Retry-After` header) |
| 500 | Internal error (generic message to client, full error logged) |

A `recovery` middleware catches panics, logs the stack trace, and returns 500.

---

## 13. Data Retention

If `DATA_RETENTION_DAYS > 0`, a background goroutine runs daily at midnight UTC and deletes rows from `clicks` where `clicked_at < NOW() - retention_days`. The `click_count` on `links` is NOT decremented (it represents lifetime total).

---

## 14. Frontend

### 14.1 Tech Stack

- React 18+ with TypeScript
- Vite for build/dev
- Tailwind CSS for styling
- React Router (only `/` dashboard + 404)
- `date-fns` for date formatting

### 14.2 Pages

**Dashboard (`/`)**:
- **Shorten form** at top: URL input, optional custom code input, optional expiration dropdown, optional tag multi-select
- **Result display**: short URL with copy button shown after creation
- **Link table**: columns — Short URL, Original URL (truncated), Created, Clicks, Tags, Status, Actions
- **Actions per row**: Copy, QR Code, Analytics expand, Activate/Deactivate, Delete
- **Above table**: search bar, sort dropdown, active/all filter, tag filter dropdown
- **Pagination** at bottom

**Bulk Shorten Modal**:
- Textarea for one URL per line
- Submit creates all, shows per-line success/error results

**Analytics Panel** (inline expand under link row):
- Total click count
- Clicks-over-time bar chart
- Period selector: 24h / 7d / 30d / all

**QR Code Modal**:
- QR code image for the short URL
- Download as PNG button

**Tag Manager** (accessible from dashboard):
- List all tags with link counts
- Create / delete tags

### 14.3 API Key Handling

- On first visit, if API returns 401, prompt user for API key
- Store in `localStorage`
- Include in all API requests as `Authorization: Bearer <key>`
- Option to clear/change key in UI

### 14.4 API Client

`src/api/client.ts`:

```typescript
createLink(url: string, customCode?: string, expiresIn?: number, tags?: string[]): Promise<Link>
createBulkLinks(urls: BulkRequest[]): Promise<BulkResponse>
getLinks(params: ListParams): Promise<PaginatedLinks>
getLink(id: number): Promise<Link>
updateLink(id: number, patch: LinkPatch): Promise<Link>
deleteLink(id: number): Promise<void>
getAnalytics(id: number, period: string): Promise<Analytics>
getQRCodeUrl(id: number, size?: number): string
getTags(): Promise<Tag[]>
createTag(name: string): Promise<Tag>
deleteTag(id: number): Promise<void>
```

Base URL from `VITE_API_URL` env var, default `http://localhost:8080`.

### 14.5 Production Serving

`npm run build` produces `frontend/dist/`. The Go backend embeds this directory using `embed.FS` and serves it via `http.FileServer`.

**Route priority in Go mux**:
1. `/api/*` — API handlers
2. Static file match in embedded `frontend/dist/` (JS, CSS, images)
3. `/:code` — redirect handler (if code exists in DB)
4. Fallback — serve `index.html` (SPA client-side routing)

---

## 15. Testing

### 15.1 Backend (Go)

**Unit tests**:
- `shortcode`: generation, charset, length, uniqueness over N iterations
- `urlcheck`: valid URLs, invalid schemes, private IPs, too-long URLs
- `handler`: each handler with `httptest.NewRecorder` and mock store (interface-based)
- `store`: tested against real in-memory SQLite (`:memory:`)

**Integration tests**:
- Full HTTP server via `httptest.NewServer`
- End-to-end: create link → follow redirect → verify analytics
- Bulk creation, pagination, search, expiration, tags

```bash
go test ./... -race -count=1
```

### 15.2 Frontend

- Vitest for unit tests
- React Testing Library for component tests
- MSW (Mock Service Worker) for API mocking
- Key tests: form submission, link table rendering, copy button, error states, tag management

### 15.3 Coverage

80% line coverage target for backend. All user-facing flows covered for frontend.

---

## 16. Build and Deploy

### 16.1 Makefile

```makefile
dev-backend:     go run ./backend/cmd/shorty
dev-frontend:    cd frontend && npm run dev
build-backend:   go build -o bin/shorty ./backend/cmd/shorty
build-frontend:  cd frontend && npm run build
build:           build-backend build-frontend
test-backend:    cd backend && go test ./... -race
test-frontend:   cd frontend && npm test
test:            test-backend test-frontend
lint:            golangci-lint run ./backend/... && cd frontend && npm run lint
migrate:         go run ./backend/cmd/shorty -migrate
```

### 16.2 Docker

Multi-stage Dockerfile:
1. **Stage 1** (Node): Build frontend → `frontend/dist/`
2. **Stage 2** (Go): Build backend with embedded frontend → single binary
3. **Stage 3** (`gcr.io/distroless/static-debian12`): Copy binary only

### 16.3 docker-compose.yml

Single service with volume for SQLite persistence and environment variables.

---

## 17. Go Dependencies

```
github.com/mattn/go-sqlite3          # SQLite driver (CGo)
golang.org/x/time/rate               # Rate limiter
github.com/skip2/go-qrcode           # QR code generation
```

HTTP routing via Go 1.22+ stdlib `http.ServeMux` (supports method and path parameter matching).

---

## 18. Edge Cases

| Case | Decision |
|------|----------|
| Same URL shortened twice | Creates separate short codes (no dedup). Avoids leaking that a URL was already shortened. |
| Redirect loop (short URL → another short URL on same instance) | Allowed. Single 301 hop; browser follows chain. |
| Unicode in URLs | Accepted. Go `url.Parse` handles percent-encoding. |
| Trailing slash on short code | `/aB3xYz/` treated same as `/aB3xYz` (strip in middleware). |
| Empty DB on first run | Migration runs automatically on startup. |
| Concurrent writes | Single write connection + `busy_timeout=5000`. WAL mode ensures reads aren't blocked. |
| Click recording fails | Logged at error level, redirect still succeeds (best-effort). |
| Short code exhaustion | Handled by length escalation (section 4). Practically impossible at 62^6. |
| Tag referenced by links is deleted | Cascade delete removes link_tags rows. Links remain, just lose the tag. |

---

## 19. Out of Scope (v1)

- Multi-user authentication / user registration
- API keys per user
- Custom domains
- Webhook on click
- CSV export
- FTS5 full-text search
- Link preview / Open Graph metadata
