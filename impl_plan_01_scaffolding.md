# Phase 1: Project Scaffolding — Implementation Plan

## Summary

Set up the buildable Go project with configuration loading, domain models, database initialization with migrations, dual read/write connection pools, and a minimal Echo server. After this phase, `make dev-backend` starts a server that creates and migrates the SQLite database.

---

## Step 1: Initialize Go Module

```bash
cd backend
go mod init shorty
```

Add dependencies:

```bash
go get github.com/labstack/echo/v4@latest
go get modernc.org/sqlite@latest
go get golang.org/x/time/rate@latest
go get github.com/skip2/go-qrcode@latest
go get github.com/joho/godotenv@latest
```

These satisfy S18 (Go Dependencies).

---

## Step 2: Configuration — `backend/internal/config/config.go`

Loads `.env` file (if present) via `godotenv.Load()`, then reads environment variables with defaults. Env vars take precedence over `.env` values (S11). Server refuses to start if `API_KEY` is empty or unset (S5).

### Struct Definition

```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                    int      // PORT, default 8080
	BaseURL                 string   // BASE_URL, default "http://localhost:8080"
	DBPath                  string   // DB_PATH, default "./shorty.db"
	APIKey                  string   // API_KEY, required
	LogLevel                string   // LOG_LEVEL, default "info"
	CORSAllowedOrigins      []string // CORS_ALLOWED_ORIGINS, default ["http://localhost:5173"]
	DefaultCodeLength       int      // DEFAULT_CODE_LENGTH, default 6
	MaxBulkURLs             int      // MAX_BULK_URLS, default 50
	ClickBufferSize         int      // CLICK_BUFFER_SIZE, default 10000
	ClickFlushInterval      int      // CLICK_FLUSH_INTERVAL, default 1 (seconds)
	RateLimitEnabled        bool     // RATE_LIMIT_ENABLED, default true
	TrustedProxies          []string // TRUSTED_PROXIES, comma-separated CIDRs
	GoogleSafeBrowsingKey   string   // GOOGLE_SAFE_BROWSING_API_KEY, default ""
	DataRetentionDays       int      // DATA_RETENTION_DAYS, default 0
}
```

### Function Signatures

```go
// Load reads .env (if present) and parses all environment variables.
// Returns error if API_KEY is missing or empty.
func Load() (*Config, error)
```

### Implementation Details

1. Call `godotenv.Load()` — ignore error if `.env` file doesn't exist (check with `os.IsNotExist`).
2. Read each env var with `os.Getenv`, apply defaults.
3. For `API_KEY`: if empty string after trimming, return `fmt.Errorf("API_KEY environment variable is required")`.
4. Parse `PORT`, `DEFAULT_CODE_LENGTH`, `MAX_BULK_URLS`, `CLICK_BUFFER_SIZE`, `CLICK_FLUSH_INTERVAL`, `DATA_RETENTION_DAYS` with `strconv.Atoi`; return error on invalid integer.
5. Parse `RATE_LIMIT_ENABLED` with `strconv.ParseBool`; default `true` if empty.
6. Split `CORS_ALLOWED_ORIGINS` and `TRUSTED_PROXIES` on commas, trim whitespace, filter empty strings.
7. Strip trailing slash from `BaseURL`.

### Helper

```go
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envIntOrDefault(key string, defaultVal int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal, nil
	}
	return strconv.Atoi(v)
}
```

---

## Step 3: Domain Models — `backend/internal/model/model.go`

Structs with JSON tags matching API response shapes (S3.1, S6.2 response format).

```go
package model

import "time"

type Link struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	ShortURL    string     `json:"short_url"`    // computed, not stored in DB
	OriginalURL string     `json:"original_url"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`   // null = never expires
	IsActive    bool       `json:"is_active"`
	ClickCount  int64      `json:"click_count"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Tags        []string   `json:"tags"`         // populated from link_tags join
}

type Click struct {
	ID        int64     `json:"id"`
	LinkID    int64     `json:"link_id"`
	ClickedAt time.Time `json:"clicked_at"`
}

type Tag struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type TagWithCount struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	LinkCount int64     `json:"link_count"`
}
```

### Notes

- `Link.ShortURL` is not a DB column — it is constructed from `BaseURL + "/" + Code` when building API responses.
- `Link.IsActive` maps to the `is_active INTEGER` column (0/1 in SQLite, bool in Go).
- `Link.ExpiresAt` is a pointer to handle SQL NULL.
- `Link.Tags` is populated by a separate query or join, not directly from the `links` table.

---

## Step 4: Migration SQL — `backend/migrations/001_init.sql`

Copy the exact SQL from S3.3:

```sql
CREATE TABLE IF NOT EXISTS links (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    code         TEXT    UNIQUE NOT NULL,
    original_url TEXT    NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   DATETIME,
    is_active    INTEGER NOT NULL DEFAULT 1,
    click_count  INTEGER NOT NULL DEFAULT 0,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
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

CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);
INSERT INTO schema_version (version) VALUES (1);
```

Embed migrations using `//go:embed`:

```go
//go:embed migrations/*.sql
var migrationsFS embed.FS
```

This directive goes in a file within `backend/` that can access the `migrations/` directory — either `backend/cmd/shorty/main.go` or a dedicated `backend/migrations.go` file. The `embed.FS` is passed to the store's migration runner.

---

## Step 5: Store Interface — `backend/internal/store/store.go`

Define the full interface upfront. Methods will be implemented incrementally across phases.

```go
package store

import (
	"context"

	"shorty/internal/model"
)

// ListParams holds query parameters for listing links (S6.4).
type ListParams struct {
	Page    int
	PerPage int
	Search  string
	Sort    string // "created_at", "click_count", "expires_at"
	Order   string // "asc", "desc"
	Active  *bool  // nil = all, true = active only, false = inactive only
	Tag     string
}

// ListResult holds paginated link results.
type ListResult struct {
	Links []model.Link
	Total int
}

// ClickEvent is sent through the async click channel.
type ClickEvent struct {
	LinkID    int64
}

type Store interface {
	// Links
	CreateLink(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error)
	GetLinkByCode(ctx context.Context, code string) (*model.Link, error)
	GetLinkByID(ctx context.Context, id int64) (*model.Link, error)
	ListLinks(ctx context.Context, params ListParams) (*ListResult, error)
	UpdateLink(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error)
	DeactivateExpiredLink(ctx context.Context, id int64) error
	DeleteLink(ctx context.Context, id int64) error
	CodeExists(ctx context.Context, code string) (bool, error)

	// Clicks
	BatchInsertClicks(ctx context.Context, events []ClickEvent) error

	// Tags
	CreateTag(ctx context.Context, name string) (*model.Tag, error)
	ListTags(ctx context.Context) ([]model.TagWithCount, error)
	DeleteTag(ctx context.Context, id int64) error
	SetLinkTags(ctx context.Context, linkID int64, tagNames []string) error
	GetLinkTags(ctx context.Context, linkID int64) ([]string, error)
	GetLinksTagsBatch(ctx context.Context, linkIDs []int64) (map[int64][]string, error)

	// Analytics
	GetClicksByDay(ctx context.Context, linkID int64, since string) ([]DayCount, error)
	GetClicksByHour(ctx context.Context, linkID int64, since string) ([]HourCount, error)
	GetPeriodClickCount(ctx context.Context, linkID int64, since string) (int, error)

	// Retention
	DeleteOldClicks(ctx context.Context, beforeDays int) (int64, error)

	// Lifecycle
	Close() error
}

type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type HourCount struct {
	Hour  string `json:"hour"`
	Count int    `json:"count"`
}
```

---

## Step 6: SQLite Implementation — `backend/internal/store/sqlite.go`

### Struct

```go
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	writeDB *sql.DB // single writer connection
	readDB  *sql.DB // read-only pool
}
```

### Constructor

```go
// New opens read and write connection pools and applies pending migrations.
// migrationsFS contains embedded SQL migration files.
func New(dbPath string, migrationsFS embed.FS) (*SQLiteStore, error)
```

#### Implementation Details

1. **Open write connection**:
   ```go
   writeDB, err := sql.Open("sqlite", dbPath)
   writeDB.SetMaxOpenConns(1) // S9.3: SQLite single-writer limitation
   ```

2. **Open read connection** (with `?mode=ro` query parameter):
   ```go
   readDB, err := sql.Open("sqlite", dbPath+"?mode=ro")
   readDB.SetMaxOpenConns(4) // S9.3
   ```

3. **Apply PRAGMAs** on both connections (S3.5). Since `database/sql` manages a pool, PRAGMAs must be set via a `ConnInitHook` or by executing them immediately after open and relying on `SetMaxOpenConns(1)` for the write pool. For the read pool, use `db.Conn(ctx)` to get each connection and set PRAGMAs, or use the `_pragma` query parameter in the DSN:
   ```
   dbPath + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-64000)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)"
   ```
   Check `modernc.org/sqlite` driver documentation — it supports `_pragma` DSN parameters. If not, execute PRAGMAs via `writeDB.Exec()` immediately after open (works reliably with `MaxOpenConns=1`).

   PRAGMAs to set (S3.5):
   ```sql
   PRAGMA journal_mode=WAL;
   PRAGMA synchronous=NORMAL;
   PRAGMA cache_size=-64000;
   PRAGMA busy_timeout=5000;
   PRAGMA foreign_keys=ON;
   PRAGMA temp_store=MEMORY;
   ```

4. **Run migrations** using `runMigrations(writeDB, migrationsFS)`.

### Migration Runner

```go
func runMigrations(db *sql.DB, migrationsFS embed.FS) error
```

#### Logic (S3.4)

1. Check if `schema_version` table exists:
   ```sql
   SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version';
   ```
2. If it doesn't exist, current version = 0.
3. If it exists, read version:
   ```sql
   SELECT version FROM schema_version LIMIT 1;
   ```
   If no rows, version = 0.
4. List migration files from `migrationsFS` under `migrations/` directory. Parse version number from filename prefix (e.g., `001` from `001_init.sql`).
5. Sort migration files by version number ascending.
6. For each migration with version > current:
   a. Read file contents.
   b. Begin transaction.
   c. Execute the SQL.
   d. If `schema_version` table didn't exist before (version was 0 and this is the first migration), the migration SQL itself creates the table and inserts the version — no separate update needed.
   e. If `schema_version` already existed, update: `UPDATE schema_version SET version = ?`.
   f. Commit transaction. On error, rollback and return error.
7. Log each applied migration.

### Close

```go
func (s *SQLiteStore) Close() error {
	if err := s.readDB.Close(); err != nil {
		return err
	}
	return s.writeDB.Close()
}
```

### Stub Methods

All `Store` interface methods should be present as stubs returning `fmt.Errorf("not implemented")` so the project compiles. They will be implemented in later phases.

---

## Step 7: Main Entrypoint — `backend/cmd/shorty/main.go`

```go
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"

	"shorty/internal/config"
	"shorty/internal/store"
)

//go:embed ../../migrations/*.sql
var migrationsFS embed.FS
```

**Note on embed path**: The `//go:embed` path is relative to the file's location. Since `main.go` is at `backend/cmd/shorty/main.go` and migrations are at `backend/migrations/`, the relative path is `../../migrations/*.sql`. Alternatively, place the embed directive in a file at `backend/` root level and pass it in.

### Flags

```go
var migrateOnly = flag.Bool("migrate", false, "Run migrations and exit")
```

### Main Function Flow

```go
func main() {
	flag.Parse()

	// 1. Load config — fatal on error (S5: server refuses to start if API_KEY unset)
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// 2. Configure slog level from cfg.LogLevel
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	// 3. Open store (creates DB, applies migrations)
	db, err := store.New(cfg.DBPath, migrationsFS)
	if err != nil {
		slog.Error("failed to open store", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// 4. If -migrate flag, exit after migrations
	if *migrateOnly {
		slog.Info("migrations applied successfully")
		return
	}

	// 5. Create Echo instance
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// 6. Register health check (no auth) — S6.13
	e.GET("/api/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status":  "ok",
			"version": "1.0.0",
		})
	})

	// 7. Start server
	addr := fmt.Sprintf(":%d", cfg.Port)
	slog.Info("starting server", "addr", addr, "base_url", cfg.BaseURL)

	go func() {
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// 8. Graceful shutdown (S10)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
```

---

## Step 8: Makefile — `Makefile` (project root)

```makefile
.PHONY: dev-backend migrate

dev-backend:
	cd backend && go run ./cmd/shorty

migrate:
	cd backend && go run ./cmd/shorty -migrate
```

---

## Step 9: Supporting Files

### `.env.example` (project root)

```
PORT=8080
BASE_URL=http://localhost:8080
DB_PATH=./shorty.db
API_KEY=change-me-to-a-secure-random-string
LOG_LEVEL=info
CORS_ALLOWED_ORIGINS=http://localhost:5173
DEFAULT_CODE_LENGTH=6
MAX_BULK_URLS=50
CLICK_BUFFER_SIZE=10000
CLICK_FLUSH_INTERVAL=1
RATE_LIMIT_ENABLED=true
TRUSTED_PROXIES=
GOOGLE_SAFE_BROWSING_API_KEY=
DATA_RETENTION_DAYS=0
```

### `.gitignore` (project root)

```
.env
*.db
bin/
frontend/dist/
frontend/node_modules/
```

---

## Error Handling

- `config.Load()` returns an error if `API_KEY` is empty. `main()` logs fatal and exits with code 1.
- `store.New()` returns an error if the DB file can't be opened or migrations fail. Same fatal exit.
- Migration failures rollback the transaction. The error includes the migration filename and the SQL error.
- All `slog` output is structured JSON (S12).

---

## Verification Checklist

1. Set `API_KEY=test123` in environment or `.env` file.
2. Run `make dev-backend`.
3. Confirm: server logs `starting server` JSON message.
4. Confirm: `shorty.db` file is created in the working directory.
5. Inspect DB: `sqlite3 shorty.db ".tables"` shows `links`, `clicks`, `tags`, `link_tags`, `schema_version`.
6. Inspect DB: `sqlite3 shorty.db "SELECT version FROM schema_version"` returns `1`.
7. Confirm: `curl http://localhost:8080/api/health` returns `{"status":"ok","version":"1.0.0"}`.
8. Confirm: running without `API_KEY` set exits with a fatal error message.
9. Confirm: `make migrate` runs migrations and exits cleanly.
10. Confirm: running `make migrate` again (idempotent) completes without error.
