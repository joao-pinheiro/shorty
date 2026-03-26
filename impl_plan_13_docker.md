# Phase 13: Docker + Deployment

## Summary

Creates the multi-stage Dockerfile, docker-compose.yml for local/production deployment, and a documented `.env.example`. The Docker image produces a single static binary on a distroless base with no libc dependency. References: S17.2, S17.3, S11, S5.

**Depends on**: Phase 12 (production build with embedded frontend).

---

## Files to Create/Modify

| File | Action |
|------|--------|
| `Dockerfile` | Create |
| `docker-compose.yml` | Create |
| `.env.example` | Create |
| `.dockerignore` | Create |
| `Makefile` | Modify — add docker targets |

---

## Step 1: Dockerfile

**File**: `Dockerfile` (repo root)

Three-stage build per S17.2:

1. **Stage 1 (Node)**: Build frontend.
2. **Stage 2 (Go)**: Build backend with embedded frontend, `CGO_ENABLED=0`.
3. **Stage 3 (distroless)**: Copy binary only.

```dockerfile
# ============================================================
# Stage 1: Build frontend
# ============================================================
FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend

# Install dependencies first (layer caching)
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

# Copy source and build
COPY frontend/ ./
RUN npm run build

# ============================================================
# Stage 2: Build backend with embedded frontend
# ============================================================
FROM golang:1.25-alpine AS backend-builder

WORKDIR /app

# Install dependencies first (layer caching)
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download

# Copy backend source
COPY backend/ ./backend/

# Copy built frontend into the embed location
COPY --from=frontend-builder /app/frontend/dist ./backend/cmd/shorty/dist

# Build static binary
RUN cd backend && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /shorty \
    ./cmd/shorty

# ============================================================
# Stage 3: Minimal runtime
# ============================================================
FROM gcr.io/distroless/static-debian12

# Copy the static binary
COPY --from=backend-builder /shorty /shorty

# Copy migrations (needed at runtime for auto-migration on startup)
COPY --from=backend-builder /app/backend/migrations /migrations

# SQLite data directory
VOLUME ["/data"]

# Default port
EXPOSE 8080

# Run as non-root (distroless provides user nonroot:65534)
USER nonroot:nonroot

ENTRYPOINT ["/shorty"]
```

### Key Decisions

- **`node:20-alpine`** and **`golang:1.25-alpine`**: Alpine variants for smaller build stages.
- **`CGO_ENABLED=0`**: Required because we use `modernc.org/sqlite` (pure Go, no CGo). This produces a fully static binary.
- **`-ldflags="-s -w"`**: Strip debug info and DWARF symbols to reduce binary size.
- **`gcr.io/distroless/static-debian12`**: Minimal runtime image. No shell, no package manager, no libc. Works because the binary is fully static.
- **`USER nonroot:nonroot`**: Run as non-root for security. Distroless provides this user.
- **Migrations directory**: Copied separately because the migration runner reads `.sql` files at startup (S3.4). If migrations are embedded in the Go binary via `embed.FS`, this COPY is not needed — adjust based on Phase 1 implementation.
- **No `.env` file in image**: Per S11 security note, `.env` must not be copied into Docker images. Use environment variables or Docker secrets.

### Layer Caching Strategy

- `package.json` + `package-lock.json` copied before source → `npm ci` layer cached unless dependencies change.
- `go.mod` + `go.sum` copied before source → `go mod download` layer cached unless dependencies change.
- Source changes only rebuild the final build steps.

---

## Step 2: .dockerignore

**File**: `.dockerignore` (repo root)

```
# Git
.git
.gitignore

# Dependencies (rebuilt in Docker)
backend/vendor
frontend/node_modules

# Build artifacts
bin/
frontend/dist/
backend/cmd/shorty/dist/

# Environment and data (must not be in image)
.env
*.db
*.db-wal
*.db-shm

# IDE
.vscode/
.idea/
*.swp
*.swo

# Docker
Dockerfile
docker-compose.yml
.dockerignore

# Documentation
*.md
!backend/migrations/*.sql

# Test artifacts
coverage.out
```

---

## Step 3: docker-compose.yml

**File**: `docker-compose.yml` (repo root)

Per S17.3: single service with volume for SQLite persistence and environment variables.

```yaml
services:
  shorty:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: shorty
    ports:
      - "${PORT:-8080}:8080"
    environment:
      - PORT=8080
      - BASE_URL=${BASE_URL:-http://localhost:8080}
      - DB_PATH=/data/shorty.db
      - API_KEY=${API_KEY:?API_KEY is required}
      - LOG_LEVEL=${LOG_LEVEL:-info}
      - CORS_ALLOWED_ORIGINS=${CORS_ALLOWED_ORIGINS:-}
      - DEFAULT_CODE_LENGTH=${DEFAULT_CODE_LENGTH:-6}
      - MAX_BULK_URLS=${MAX_BULK_URLS:-50}
      - CLICK_BUFFER_SIZE=${CLICK_BUFFER_SIZE:-10000}
      - CLICK_FLUSH_INTERVAL=${CLICK_FLUSH_INTERVAL:-1}
      - RATE_LIMIT_ENABLED=${RATE_LIMIT_ENABLED:-true}
      - TRUSTED_PROXIES=${TRUSTED_PROXIES:-}
      - GOOGLE_SAFE_BROWSING_API_KEY=${GOOGLE_SAFE_BROWSING_API_KEY:-}
      - DATA_RETENTION_DAYS=${DATA_RETENTION_DAYS:-0}
    volumes:
      - shorty-data:/data
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "/shorty", "-healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 5s

volumes:
  shorty-data:
    driver: local
```

### Notes

- **`API_KEY=${API_KEY:?...}`**: The `?` syntax causes docker compose to fail with an error if `API_KEY` is not set. This mirrors the server's own requirement (S5, S11).
- **`DB_PATH=/data/shorty.db`**: Points to the mounted volume so data persists across container restarts.
- **`volumes: shorty-data`**: Named Docker volume. Data persists until explicitly removed with `docker volume rm`.
- **`restart: unless-stopped`**: Auto-restart on crash, but not after explicit `docker stop`.
- **Healthcheck**: Assumes the binary supports a `-healthcheck` flag that hits `/api/health` and exits 0/1. If not implemented, use a simpler alternative:
  ```yaml
  healthcheck:
    test: ["NONE"]
  ```
  Or remove the healthcheck entirely since distroless has no `curl` or `wget`. Alternative: use the Go binary itself as a health check client (add a `-healthcheck` CLI flag that makes an HTTP request to `localhost:PORT/api/health`).

### Healthcheck Alternative (without -healthcheck flag)

Since distroless has no shell or HTTP clients, either:

1. **Add `-healthcheck` flag to the Go binary** (recommended):
   ```go
   if *healthcheck {
       resp, err := http.Get(fmt.Sprintf("http://localhost:%s/api/health", cfg.Port))
       if err != nil || resp.StatusCode != 200 {
           os.Exit(1)
       }
       os.Exit(0)
   }
   ```

2. **Remove healthcheck from compose** and rely on restart policy.

3. **Use a multi-stage runtime with shell** (not recommended — defeats purpose of distroless).

---

## Step 4: .env.example

**File**: `.env.example` (repo root)

```bash
# =============================================================================
# Shorty Configuration
# Copy to .env and fill in required values.
# Env vars override .env values. Do NOT commit .env to version control.
# =============================================================================

# REQUIRED: API key for authentication. Server refuses to start if unset.
API_KEY=change-me-to-a-strong-random-key

# HTTP listen port
PORT=8080

# Base URL used to construct short URLs in API responses.
# Must match the publicly accessible URL (including port if non-standard).
BASE_URL=http://localhost:8080

# SQLite database file path
DB_PATH=./shorty.db

# Log level: debug, info, warn, error
LOG_LEVEL=info

# CORS allowed origins (comma-separated). Set to your frontend URL in production.
# Leave empty to use default (http://localhost:5173 for dev).
CORS_ALLOWED_ORIGINS=

# Generated short code length (default: 6, escalates to 7 on collision)
DEFAULT_CODE_LENGTH=6

# Maximum URLs per bulk create request
MAX_BULK_URLS=50

# Async click recording buffer size
CLICK_BUFFER_SIZE=10000

# Click batch flush interval in seconds
CLICK_FLUSH_INTERVAL=1

# Enable/disable rate limiting (true/false)
RATE_LIMIT_ENABLED=true

# Trusted reverse proxy CIDRs (comma-separated).
# When set, client IP is extracted from X-Forwarded-For header.
# Example: 172.16.0.0/12,10.0.0.0/8
TRUSTED_PROXIES=

# Google Safe Browsing API key (optional).
# If set, URLs are checked against Google Safe Browsing before shortening.
GOOGLE_SAFE_BROWSING_API_KEY=

# Days to retain click data (0 = keep forever).
# Old click rows are purged daily at midnight UTC.
# Note: link click_count is NOT decremented (lifetime counter).
DATA_RETENTION_DAYS=0
```

---

## Step 5: Update Makefile with Docker Targets

**File**: `Makefile` (modify — add these targets)

```makefile
# Docker
docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

docker-clean:
	docker compose down -v
```

### Full Makefile (consolidated from Phase 12 + Phase 13)

```makefile
.PHONY: dev-backend dev-frontend build-backend build-frontend build \
        test-backend test-frontend test lint migrate \
        docker-build docker-up docker-down docker-logs docker-clean

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
	cd backend && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../bin/shorty ./cmd/shorty

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

# Docker
docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

docker-clean:
	docker compose down -v
```

---

## Step 6: Healthcheck CLI Flag (Optional but Recommended)

**File**: `backend/cmd/shorty/main.go` (modify)

Add a `-healthcheck` flag so the Docker healthcheck can work in distroless:

```go
func main() {
	healthcheck := flag.Bool("healthcheck", false, "Run health check and exit")
	migrateOnly := flag.Bool("migrate", false, "Run migrations and exit")
	flag.Parse()

	if *healthcheck {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		resp, err := http.Get(fmt.Sprintf("http://localhost:%s/api/health", port))
		if err != nil {
			fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "health check returned status %d\n", resp.StatusCode)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// ... rest of main
}
```

---

## Step 7: Production Deployment Considerations

### Running with Docker Compose

```bash
# Set API key (required)
export API_KEY=$(openssl rand -hex 32)

# Start
docker compose up -d

# Verify
curl http://localhost:8080/api/health
# → {"status":"ok","version":"1.0.0"}

# View logs
docker compose logs -f

# Stop
docker compose down
```

### Running Behind a Reverse Proxy (nginx/Caddy/Traefik)

Set these environment variables:

```bash
BASE_URL=https://sho.rt              # Public URL (used in short_url responses)
TRUSTED_PROXIES=172.16.0.0/12       # Docker network CIDR
CORS_ALLOWED_ORIGINS=https://sho.rt  # Match public URL
```

### Volume Backup

```bash
# Backup SQLite database
docker compose exec shorty cat /data/shorty.db > backup.db

# Or copy from volume directly
docker cp shorty:/data/shorty.db ./backup.db
```

Note: For consistent backups of a live database, it's better to use SQLite's `.backup` command or the Go binary's backup functionality if implemented. WAL mode means the `.db` file alone may not be sufficient — also back up `.db-wal` and `.db-shm` if they exist, or ensure WAL is checkpointed before backup.

---

## Verification Checklist

1. **Docker build**:
   - `docker compose build` completes without errors.
   - Final image is small (< 30MB expected — distroless base + static Go binary).
   - `docker images shorty` shows the image.

2. **Docker run**:
   - `API_KEY=test docker compose up -d` starts the container.
   - `docker compose ps` shows the service as healthy (after start_period).
   - `curl http://localhost:8080/api/health` returns `{"status":"ok","version":"1.0.0"}`.

3. **Data persistence**:
   - Create a link via API.
   - `docker compose down && docker compose up -d` — container restarts.
   - The link still exists (query the API).

4. **Data destruction**:
   - `docker compose down -v` removes the volume.
   - Start again — database is empty (fresh migration).

5. **No secrets in image**:
   - `docker history shorty-shorty` — no layer contains `.env` or API keys.
   - The `.dockerignore` excludes `.env`.

6. **Non-root execution**:
   - Container runs as user `nonroot` (UID 65534).
   - SQLite file at `/data/shorty.db` is writable (volume permissions).

7. **Environment variable override**:
   - `PORT=9090 API_KEY=test docker compose up` — service listens on 9090.
   - `BASE_URL=https://example.com API_KEY=test docker compose up` — created links have `https://example.com/{code}` in responses.
