# Shorty

Self-hosted URL shortener with a Go backend, React frontend, and SQLite database. Deploys as a single binary.

## Quick Start

### Docker (recommended)

```bash
cp .env.example .env
# Edit .env — at minimum, set API_KEY to a strong random string

docker compose up -d
```

The service is available at `http://localhost:8080`.

### From source

**Prerequisites:** Go 1.25+, Node.js 20+

```bash
# 1. Configure
cp .env.example .env
# Edit .env — set API_KEY

# 2. Install frontend dependencies
cd frontend && npm ci && cd ..

# 3. Start backend (port 8080)
make dev-backend

# 4. Start frontend dev server (port 5173, proxies API to 8080)
make dev-frontend
```

During development, use the frontend at `http://localhost:5173`. It proxies API requests to the backend.

### Production build

```bash
make build
# Produces bin/shorty — a single static binary with the frontend embedded

API_KEY=your-secret ./bin/shorty
```

## API

All management endpoints require `Authorization: Bearer <API_KEY>`.

### Shorten a URL

```bash
curl -X POST http://localhost:8080/api/v1/links \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{"url": "https://example.com/very-long-path"}'
```

Response:
```json
{
  "id": 1,
  "code": "aB3xYz",
  "short_url": "http://localhost:8080/aB3xYz",
  "original_url": "https://example.com/very-long-path",
  "created_at": "2026-03-26T10:00:00Z",
  "expires_at": null,
  "is_active": true,
  "click_count": 0,
  "updated_at": "2026-03-26T10:00:00Z",
  "tags": []
}
```

### Custom code + expiration + tags

```bash
curl -X POST http://localhost:8080/api/v1/links \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "url": "https://example.com",
    "custom_code": "my-link",
    "expires_in": 86400,
    "tags": ["marketing"]
  }'
```

### Redirect

```
GET http://localhost:8080/aB3xYz  →  302 Found  →  https://example.com/very-long-path
```

No authentication required.

### API Endpoints

| Method | Path | Auth | Description |
|--------|------|:----:|-------------|
| GET | `/:code` | No | Redirect to original URL |
| GET | `/:code/qr` | No | QR code PNG for the short URL |
| POST | `/api/v1/links` | Yes | Create short link |
| POST | `/api/v1/links/bulk` | Yes | Bulk create (max 50) |
| GET | `/api/v1/links` | Yes | List links (paginated, searchable) |
| GET | `/api/v1/links/:id` | Yes | Get single link |
| PATCH | `/api/v1/links/:id` | Yes | Update link (activate/deactivate, expiry, tags) |
| DELETE | `/api/v1/links/:id` | Yes | Delete link |
| GET | `/api/v1/links/:id/analytics` | Yes | Click analytics (24h/7d/30d/all) |
| GET | `/api/v1/links/:id/qr` | Yes | QR code PNG (authenticated) |
| GET | `/api/v1/tags` | Yes | List all tags |
| POST | `/api/v1/tags` | Yes | Create tag |
| DELETE | `/api/v1/tags/:id` | Yes | Delete tag |
| GET | `/api/health` | No | Health check |

## Configuration

All configuration is via environment variables. Copy `.env.example` to `.env` to get started.

| Variable | Default | Description |
|----------|---------|-------------|
| `API_KEY` | **(required)** | API key for authentication |
| `PORT` | `8080` | HTTP listen port |
| `BASE_URL` | `http://localhost:8080` | Public URL for constructing short links |
| `DB_PATH` | `./shorty.db` | SQLite database file path |
| `LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173` | Comma-separated allowed origins |
| `DEFAULT_CODE_LENGTH` | `6` | Generated short code length |
| `MAX_BULK_URLS` | `50` | Max URLs per bulk request |
| `CLICK_BUFFER_SIZE` | `10000` | Async click recording buffer |
| `CLICK_FLUSH_INTERVAL` | `1` | Click batch flush interval (seconds) |
| `RATE_LIMIT_ENABLED` | `true` | Enable/disable rate limiting |
| `TRUSTED_PROXIES` | | Comma-separated CIDRs for X-Forwarded-For |
| `GOOGLE_SAFE_BROWSING_API_KEY` | | Optional Safe Browsing URL checks |
| `DATA_RETENTION_DAYS` | `0` | Days to keep click data (0 = forever) |

## Development

```bash
make dev-backend       # Run backend with hot reload (go run)
make dev-frontend      # Run frontend dev server (Vite)
make test              # Run all tests
make test-backend      # Run backend tests only
make lint              # Run go vet
make migrate           # Run database migrations and exit
```

## Testing

```bash
cd backend && go test ./... -race -count=1
```

All testable packages have 80%+ code coverage.

## Docker

```bash
make docker-build      # Build image
make docker-up         # Start container
make docker-down       # Stop container
make docker-logs       # Follow logs
make docker-clean      # Stop and remove volumes
```

The Docker image uses a multi-stage build (Node + Go + distroless) producing a minimal image with a single static binary.

Data is persisted in a Docker volume at `/data/shorty.db`.

## Architecture

```
Go Backend (Echo v4)          React Frontend (Vite + Tailwind)
├── /api/v1/*  (REST API)     ├── Dashboard (shorten, list, search)
├── /:code     (redirect)     ├── Analytics panel (Recharts)
├── /:code/qr  (public QR)    ├── Bulk shorten modal
└── /*         (SPA fallback)  ├── QR code modal
                               └── Tag management
         │
    SQLite (WAL mode)
    ├── links, clicks
    ├── tags, link_tags
    └── schema_version
```

- **Single binary**: Frontend is embedded in the Go binary via `embed.FS`
- **SQLite**: WAL mode with separate read (4 conn) and write (1 conn) pools
- **Async clicks**: Non-blocking buffered channel with batch inserts
- **Rate limiting**: Per-IP token bucket with configurable trusted proxies
- **Auth**: Single API key with constant-time comparison

## License

MIT
