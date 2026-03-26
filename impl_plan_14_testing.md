# Phase 14: Testing

## Summary

Comprehensive test suite for backend (Go) and frontend (TypeScript/React). Backend targets 80% line coverage (S16.3) with unit tests against mock/in-memory stores and integration tests via `httptest`. Frontend covers all user-facing flows with Vitest, React Testing Library, and MSW. References: S16.1, S16.2, S16.3.

**Depends on**: All previous phases (tests cover the full codebase).

---

## Files to Create/Modify

### Backend Test Files

| File | Tests |
|------|-------|
| `backend/internal/store/sqlite_test.go` | Store layer against `:memory:` SQLite |
| `backend/internal/handler/links_test.go` | Link CRUD handlers |
| `backend/internal/handler/redirect_test.go` | Redirect handler |
| `backend/internal/handler/bulk_test.go` | Bulk create handler |
| `backend/internal/handler/analytics_test.go` | Analytics handler |
| `backend/internal/handler/tags_test.go` | Tag handlers |
| `backend/internal/handler/health_test.go` | Health check handler |
| `backend/internal/handler/middleware_test.go` | Auth, rate limit, security headers, CORS |
| `backend/internal/handler/integration_test.go` | Full server integration tests |
| `backend/internal/shortcode/shortcode_test.go` | Already exists from Phase 2 |
| `backend/internal/urlcheck/urlcheck_test.go` | Already exists from Phase 2 |

### Frontend Test Files

| File | Tests |
|------|-------|
| `frontend/src/test/setup.ts` | MSW setup + test globals |
| `frontend/src/test/handlers.ts` | MSW request handlers |
| `frontend/src/components/ShortenForm.test.tsx` | Form submission, validation |
| `frontend/src/components/LinkTable.test.tsx` | Rendering, pagination, sort |
| `frontend/src/components/CopyButton.test.tsx` | Clipboard interaction |
| `frontend/src/components/AnalyticsPanel.test.tsx` | Period switching, chart |
| `frontend/src/components/BulkShortenModal.test.tsx` | Bulk submit, results |
| `frontend/src/components/QRCodeModal.test.tsx` | QR display, download |
| `frontend/src/components/TagManager.test.tsx` | Tag CRUD |
| `frontend/src/components/TagFilter.test.tsx` | Tag selection |
| `frontend/src/components/SearchBar.test.tsx` | Search, sort, filter |
| `frontend/src/api/client.test.ts` | API client, auth header, error handling |
| `frontend/src/hooks/useLinks.test.ts` | Hook state management |
| `frontend/src/hooks/useTags.test.ts` | Hook state management |

### Config Files

| File | Action |
|------|--------|
| `frontend/vitest.config.ts` | Create |
| `frontend/src/test/setup.ts` | Create |
| `Makefile` | Verify test targets |

---

## Part 1: Backend Unit Tests

### 1.1 Store Tests (`backend/internal/store/sqlite_test.go`)

Test all store methods against a real SQLite database using `:memory:` for speed and isolation. Each test function gets a fresh database.

#### Test Helper

```go
package store

import (
	"context"
	"testing"
)

// newTestStore creates a fresh in-memory SQLite store with migrations applied.
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var ctx = context.Background()
```

#### Test Cases

**Link CRUD**:

```go
func TestCreateLink(t *testing.T)
// - Create link with generated code → verify all fields returned
// - Create link with custom code → verify code matches
// - Create link with expires_at → verify expires_at set
// - Create link with duplicate code → verify error (unique constraint)

func TestGetLinkByCode(t *testing.T)
// - Get existing link → verify all fields
// - Get non-existent code → verify not-found error

func TestGetLinkByID(t *testing.T)
// - Get existing link → verify all fields
// - Get non-existent ID → verify not-found error

func TestListLinks(t *testing.T)
// - Empty DB → returns empty slice, total=0
// - Insert 25 links → page 1 per_page 20 returns 20 items, total=25
// - Page 2 returns 5 items
// - Sort by created_at desc (default) → verify order
// - Sort by click_count asc → verify order
// - Search by URL substring → returns matching links only
// - Search by code substring → returns matching links only
// - Filter active=true → returns only active links
// - Filter active=false → returns only inactive links
// - Filter by tag → returns only links with that tag
// - Combined search + tag + active filter → correct results
// - per_page exceeding 100 is capped at 100
// - Invalid sort column is rejected (whitelist validation)

func TestUpdateLink(t *testing.T)
// - Update is_active → verify changed, updated_at changed
// - Update expires_at → verify changed
// - Update tags (full replacement) → verify old tags removed, new tags set
// - Update non-existent ID → not-found error

func TestDeactivateExpiredLink(t *testing.T)
// - Deactivate link → is_active=0, updated_at updated

func TestDeleteLink(t *testing.T)
// - Delete existing link → verify removed
// - Delete cascades to clicks table
// - Delete cascades to link_tags table
// - Delete non-existent ID → not-found error
```

**Tags**:

```go
func TestCreateTag(t *testing.T)
// - Create tag → verify returned with id, name, created_at
// - Create duplicate name → conflict error
// - Create 100 tags → success; create 101st → limit error (S6.11)

func TestListTags(t *testing.T)
// - Empty → empty slice
// - Create tags, assign to links → verify link_count is correct
// - Tag with no links → link_count = 0

func TestDeleteTag(t *testing.T)
// - Delete tag → removed from tags table
// - Delete cascades: link_tags rows removed, links remain intact
// - Delete non-existent ID → not-found error

func TestSetLinkTags(t *testing.T)
// - Set tags on link → verify link_tags entries
// - Replace tags → old removed, new added
// - Set with non-existent tag names → tags auto-created
// - Set empty tags → all link_tags removed for that link

func TestGetLinkTags(t *testing.T)
// - Link with tags → returns tag names
// - Link with no tags → empty slice

func TestGetLinksTagsBatch(t *testing.T)
// - Multiple links with various tags → correct mapping
// - Empty input → empty map
```

**Clicks**:

```go
func TestBatchInsertClicks(t *testing.T)
// - Insert batch of clicks → verify rows in clicks table
// - click_count on links incremented correctly

func TestGetClicksByDay(t *testing.T)
// - Insert clicks across multiple days → verify daily buckets
// - No clicks in period → empty slice

func TestGetClicksByHour(t *testing.T)
// - Insert clicks across multiple hours → verify hourly buckets

func TestGetPeriodClickCount(t *testing.T)
// - Count matches sum of individual buckets
```

**Migration**:

```go
func TestMigrationVersioning(t *testing.T)
// - Fresh DB → version 1 after migration
// - All tables exist
// - All indexes exist
// - Re-running migration is idempotent
```

---

### 1.2 Handler Tests (Mock Store)

Handler tests use `echo.NewContext` with a mock store to test HTTP-level behavior in isolation.

#### Mock Store

```go
// backend/internal/store/mock_store.go (or in test file)
package store

type MockStore struct {
	CreateLinkFn           func(ctx context.Context, url, code string, expiresAt *time.Time) (model.Link, error)
	GetLinkByCodeFn        func(ctx context.Context, code string) (model.Link, error)
	GetLinkByIDFn          func(ctx context.Context, id int64) (model.Link, error)
	ListLinksFn            func(ctx context.Context, params ListParams) ([]model.Link, int, error)
	UpdateLinkFn           func(ctx context.Context, id int64, patch model.LinkPatch) (model.Link, error)
	DeleteLinkFn           func(ctx context.Context, id int64) error
	DeactivateExpiredLinkFn func(ctx context.Context, id int64) error
	CreateTagFn            func(ctx context.Context, name string) (model.Tag, error)
	ListTagsFn             func(ctx context.Context) ([]model.TagWithCount, error)
	DeleteTagFn            func(ctx context.Context, id int64) error
	SetLinkTagsFn          func(ctx context.Context, linkID int64, tags []string) error
	GetLinkTagsFn          func(ctx context.Context, linkID int64) ([]string, error)
	GetLinksTagsBatchFn    func(ctx context.Context, ids []int64) (map[int64][]string, error)
	BatchInsertClicksFn    func(ctx context.Context, clicks []model.Click) error
	GetClicksByDayFn       func(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error)
	GetClicksByHourFn      func(ctx context.Context, linkID int64, since time.Time) ([]model.HourCount, error)
	GetPeriodClickCountFn       func(ctx context.Context, linkID int64, since time.Time) (int, error)
	DeleteClicksOlderThanFn     func(ctx context.Context, before time.Time) (int64, error)
}

// Each method delegates to the corresponding Fn field. Nil Fn panics (test setup error).
func (m *MockStore) CreateLink(ctx context.Context, url, code string, expiresAt *time.Time) (model.Link, error) {
	return m.CreateLinkFn(ctx, url, code, expiresAt)
}
// ... repeat for all methods

func (m *MockStore) DeleteClicksOlderThan(ctx context.Context, before time.Time) (int64, error) {
	return m.DeleteClicksOlderThanFn(ctx, before)
}
```

#### Test Helper

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func newTestContext(method, path, body string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	return c, rec
}
```

#### Links Handler Tests (`backend/internal/handler/links_test.go`)

```go
func TestCreateLink_Success(t *testing.T)
// - Valid URL → 201, response has all fields, short_url constructed correctly

func TestCreateLink_CustomCode(t *testing.T)
// - Valid custom code → 201, code matches input

func TestCreateLink_WithExpiresIn(t *testing.T)
// - expires_in=3600 → 201, expires_at is ~1 hour from now

func TestCreateLink_WithTags(t *testing.T)
// - tags=["a","b"] → 201, tags in response

func TestCreateLink_InvalidURL(t *testing.T)
// - Empty URL → 400 "invalid URL: must be http or https"
// - ftp:// scheme → 400
// - No host → 400

func TestCreateLink_URLTooLong(t *testing.T)
// - 2049 char URL → 400 "URL exceeds 2048 characters"

func TestCreateLink_CustomCodeInvalid(t *testing.T)
// - "ab" (too short) → 400 "code must be 3-32 alphanumeric, dash, or underscore"
// - "has spaces" → 400
// - 33 char code → 400

func TestCreateLink_CustomCodeReserved(t *testing.T)
// - "api" → 400 "code is reserved"
// - "health" → 400
// - "admin" → 400
// - "favicon.ico" → 400

func TestCreateLink_CustomCodeConflict(t *testing.T)
// - Code already exists → 409 "code already in use"

func TestCreateLink_InvalidTag(t *testing.T)
// - Tag with spaces → 400 "invalid tag name: must be 1-50 alphanumeric, dash, or underscore"
// - Tag > 50 chars → 400

func TestCreateLink_InvalidExpiresIn(t *testing.T)
// - expires_in=0 → 400 "expires_in must be a positive integer, max 31536000 (365 days)"
// - expires_in=-1 → 400
// - expires_in=31536001 → 400

func TestListLinks_Defaults(t *testing.T)
// - No params → page=1, per_page=20, sort=created_at, order=desc

func TestListLinks_WithParams(t *testing.T)
// - All query params set → correct ListParams passed to store

func TestListLinks_ResponseShape(t *testing.T)
// - Verify response has links[], total, page, per_page fields
// - Each link has short_url constructed from BASE_URL

func TestGetLink_Success(t *testing.T)
// - Existing link → 200, full link object

func TestGetLink_NotFound(t *testing.T)
// - Non-existent ID → 404

func TestUpdateLink_Success(t *testing.T)
// - Patch is_active → 200, updated link

func TestUpdateLink_NotFound(t *testing.T)
// - Non-existent ID → 404

func TestDeleteLink_Success(t *testing.T)
// - Existing link → 204, no body

func TestDeleteLink_NotFound(t *testing.T)
// - Non-existent ID → 404
```

#### Redirect Handler Tests (`backend/internal/handler/redirect_test.go`)

```go
func TestRedirect_Success(t *testing.T)
// - Active link → 302, Location header = original_url
// - Cache-Control: private, max-age=0
// - X-Frame-Options: DENY

func TestRedirect_NotFound(t *testing.T)
// - Unknown code → 404 {"error": "not found"}

func TestRedirect_Deactivated(t *testing.T)
// - is_active=false → 410 {"error": "link is deactivated"}

func TestRedirect_Expired(t *testing.T)
// - expires_at in the past → 410 {"error": "link has expired"}
// - Verify DeactivateExpiredLink was called (lazy deactivation)

func TestRedirect_TrailingSlash(t *testing.T)
// - /code/ treated same as /code (S19)
```

#### Bulk Handler Tests (`backend/internal/handler/bulk_test.go`)

```go
func TestBulkCreate_AllSuccess(t *testing.T)
// - 3 valid URLs → 200, total=3, succeeded=3, failed=0

func TestBulkCreate_PartialFailure(t *testing.T)
// - 3 URLs (1 invalid) → 200, total=3, succeeded=2, failed=1
// - Failed item has ok=false, error message, correct 0-based index

func TestBulkCreate_EmptyList(t *testing.T)
// - Empty urls array → 400

func TestBulkCreate_ExceedsMax(t *testing.T)
// - 51 URLs → 400 (max 50, S6.3)

func TestBulkCreate_ResponseOrder(t *testing.T)
// - Results preserve input order (S6.3)
```

#### Analytics Handler Tests (`backend/internal/handler/analytics_test.go`)

```go
func TestGetAnalytics_7d(t *testing.T)
// - period=7d → response has clicks_by_day, NO clicks_by_hour

func TestGetAnalytics_24h(t *testing.T)
// - period=24h → response has clicks_by_hour, NO clicks_by_day

func TestGetAnalytics_30d(t *testing.T)
// - period=30d → clicks_by_day

func TestGetAnalytics_All(t *testing.T)
// - period=all → clicks_by_day

func TestGetAnalytics_InvalidPeriod(t *testing.T)
// - period=1y → 400

func TestGetAnalytics_NotFound(t *testing.T)
// - Non-existent link ID → 404
```

#### Tags Handler Tests (`backend/internal/handler/tags_test.go`)

```go
func TestListTags(t *testing.T)
// - Returns tags with link_count (S6.10)

func TestCreateTag_Success(t *testing.T)
// - Valid name → 201

func TestCreateTag_InvalidName(t *testing.T)
// - Spaces → 400
// - Empty → 400
// - > 50 chars → 400

func TestCreateTag_Duplicate(t *testing.T)
// - Already exists → 409

func TestCreateTag_LimitReached(t *testing.T)
// - 100 tags exist → 400 "tag limit reached (max 100)" (S6.11)

func TestDeleteTag_Success(t *testing.T)
// - Existing tag → 204

func TestDeleteTag_NotFound(t *testing.T)
// - Non-existent → 404
```

#### Health Handler Tests (`backend/internal/handler/health_test.go`)

```go
func TestHealth(t *testing.T)
// - GET /api/health → 200 {"status":"ok","version":"1.0.0"}
// - No auth required
```

---

### 1.3 Middleware Tests (`backend/internal/handler/middleware_test.go`)

```go
func TestAuthMiddleware_ValidKey(t *testing.T)
// - Authorization: Bearer <correct-key> → request proceeds (next handler called)

func TestAuthMiddleware_MissingHeader(t *testing.T)
// - No Authorization header → 401 {"error": "missing authorization header"}

func TestAuthMiddleware_InvalidKey(t *testing.T)
// - Authorization: Bearer wrong-key → 401 {"error": "invalid API key"}

func TestAuthMiddleware_MalformedHeader(t *testing.T)
// - Authorization: Basic ... → 401

func TestAuthMiddleware_PublicRoutes(t *testing.T)
// - GET /:code (redirect) does NOT require auth
// - GET /:code/qr does NOT require auth
// - GET /api/health does NOT require auth

func TestRateLimitMiddleware_AllowsNormalTraffic(t *testing.T)
// - Single request → 200, rate limit headers present

func TestRateLimitMiddleware_EnforcesLimit(t *testing.T)
// - Exceed rate → 429 {"error": "rate limit exceeded", "retry_after": N}
// - Retry-After header present

func TestRateLimitMiddleware_PerIP(t *testing.T)
// - Different IPs have independent limits

func TestRateLimitMiddleware_Headers(t *testing.T)
// - X-RateLimit-Limit present
// - X-RateLimit-Remaining decrements
// - X-RateLimit-Reset is a Unix timestamp

func TestSecurityHeaders_AllResponses(t *testing.T)
// - X-Content-Type-Options: nosniff on every response

func TestSecurityHeaders_Redirect(t *testing.T)
// - X-Frame-Options: DENY on redirect responses

func TestBodyLimit(t *testing.T)
// - Body > 1MB → 413 {"error": "request body too large"}

func TestCORS_AllowedOrigin(t *testing.T)
// - Preflight with allowed origin → correct CORS headers
// - Access-Control-Allow-Methods includes GET, POST, PATCH, DELETE, OPTIONS
// - Access-Control-Allow-Headers includes Content-Type, Authorization
// - Access-Control-Max-Age: 86400

func TestErrorHandler_ConsistentJSON(t *testing.T)
// - 404 (Echo built-in) → {"error": "..."} format
// - 405 → {"error": "..."} format
// - Panic in handler → 500 {"error": "..."} (via Recover middleware)
```

---

### 1.4 Integration Tests (`backend/internal/handler/integration_test.go`)

Full HTTP server tests using `httptest.NewServer`. These test the complete request lifecycle including middleware, routing, and store.

#### Setup

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

type testServer struct {
	server *httptest.Server
	apiKey string
	store  *store.SQLiteStore
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	apiKey := "test-api-key"
	s, err := store.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	// Register all middleware and routes exactly as main.go does
	// ...

	ts := httptest.NewServer(e)
	t.Cleanup(func() {
		ts.Close()
		s.Close()
	})

	return &testServer{server: ts, apiKey: apiKey, store: s}
}

// Helper: authenticated request
func (ts *testServer) request(t *testing.T, method, path string, body string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, ts.server.URL+path, bodyReader)
	req.Header.Set("Authorization", "Bearer "+ts.apiKey)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse // don't follow redirects
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}
```

#### Integration Test Cases

```go
func TestE2E_CreateRedirectAnalytics(t *testing.T)
// End-to-end: create link → follow redirect → verify click recorded → check analytics
// 1. POST /api/v1/links → 201, extract code
// 2. GET /:code → 302, Location = original URL
// 3. Wait for click flush (or call flush manually)
// 4. GET /api/v1/links/:id → verify click_count >= 1
// 5. GET /api/v1/links/:id/analytics?period=24h → verify period_clicks >= 1

func TestE2E_BulkCreateMixedResults(t *testing.T)
// 1. POST /api/v1/links/bulk with 3 URLs (1 invalid)
// 2. Verify total=3, succeeded=2, failed=1
// 3. Verify failed item has correct index and error message
// 4. GET /api/v1/links → 2 links exist

func TestE2E_TagLifecycle(t *testing.T)
// 1. POST /api/v1/tags → create "marketing" tag
// 2. POST /api/v1/links with tags=["marketing"] → link has tag
// 3. GET /api/v1/links?tag=marketing → returns the link
// 4. GET /api/v1/tags → "marketing" with link_count=1
// 5. DELETE /api/v1/tags/:id → tag removed
// 6. GET /api/v1/links/:id → link still exists, tags=[]

func TestE2E_LinkExpiration(t *testing.T)
// 1. POST /api/v1/links with expires_in=1 (1 second)
// 2. Wait 2 seconds
// 3. GET /:code → 410 "link has expired"
// 4. GET /api/v1/links/:id → link returned with is_active=false (management API always returns it, S6.5)

func TestE2E_DeactivateReactivate(t *testing.T)
// 1. Create link
// 2. PATCH is_active=false → 200
// 3. GET /:code → 410 "link is deactivated"
// 4. PATCH is_active=true → 200
// 5. GET /:code → 302 redirect

func TestE2E_PaginationAndSearch(t *testing.T)
// 1. Create 25 links with varied URLs
// 2. GET /api/v1/links?page=1&per_page=10 → 10 links, total=25
// 3. GET /api/v1/links?page=3&per_page=10 → 5 links
// 4. GET /api/v1/links?search=example → only matching links
// 5. GET /api/v1/links?sort=click_count&order=asc → correct order

func TestE2E_CustomCodeReservedWords(t *testing.T)
// POST /api/v1/links with custom_code="api" → 400 "code is reserved"
// POST /api/v1/links with custom_code="health" → 400
// POST /api/v1/links with custom_code="admin" → 400

func TestE2E_AuthRequired(t *testing.T)
// - All /api/v1/* endpoints without auth → 401
// - GET /:code without auth → 302 (no auth needed)
// - GET /api/health without auth → 200 (no auth needed)

func TestE2E_QRCode(t *testing.T)
// 1. Create link
// 2. GET /api/v1/links/:id/qr → 200, Content-Type: image/png
// 3. GET /:code/qr → 200, Content-Type: image/png (no auth)
// 4. GET /:code/qr?size=512 → valid PNG at larger size

func TestE2E_ConcurrentRedirects(t *testing.T)
// 1. Create link
// 2. Fire 100 concurrent GET /:code requests
// 3. All return 302
// 4. Wait for flush
// 5. Verify click_count ~= 100 (may be less if buffer drops)
```

### Data Retention Test

```go
func TestDataRetention_PurgesOldClicks(t *testing.T)
// - Create link, insert clicks with old timestamps (> retention days ago)
// - Run retention purge
// - Verify old clicks deleted, recent clicks remain
// - Verify click_count on link is NOT decremented (S14)
```

### Graceful Shutdown Test

```go
func TestGracefulShutdown_FlushesClickBuffer(t *testing.T)
// - Start server, create link, send several redirects
// - Send SIGINT to server process
// - Wait for exit
// - Open DB directly and verify all clicks were flushed
// - Verify exit code 0 on clean shutdown
```

### Rate Limit Response Test

```go
func TestRateLimit_RetryAfterHeader(t *testing.T)
// - Exceed rate limit on redirect endpoint
// - Verify 429 response includes Retry-After header
// - Verify Retry-After value is a positive integer (seconds)
```

### Sort Column Whitelist Test

```go
func TestListLinks_InvalidSortColumn(t *testing.T)
// - GET /api/v1/links?sort=DROP_TABLE → 400 {"error": "invalid sort column"}
// - Verify handler-level validation rejects sort values not in whitelist
// - Allowed values: created_at, click_count, expires_at
```

---

## Part 2: Frontend Testing

### 2.1 Test Infrastructure Setup

#### Vitest Configuration

**File**: `frontend/vitest.config.ts`

```typescript
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'lcov'],
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/test/**', 'src/**/*.test.{ts,tsx}', 'src/types.ts'],
    },
  },
});
```

#### Test Setup

**File**: `frontend/src/test/setup.ts`

```typescript
import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterAll, afterEach, beforeAll } from 'vitest';
import { server } from './handlers';

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  cleanup();
  server.resetHandlers();
});
afterAll(() => server.close());

// Mock clipboard API
Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn().mockResolvedValue(undefined),
  },
});
```

#### MSW Handlers

**File**: `frontend/src/test/handlers.ts`

```typescript
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

const BASE_URL = 'http://localhost:8080';

// Sample data
const sampleLink = {
  id: 1,
  code: 'abc123',
  short_url: `${BASE_URL}/abc123`,
  original_url: 'https://example.com/very-long-path',
  created_at: '2026-03-26T10:00:00Z',
  expires_at: null,
  is_active: true,
  click_count: 42,
  updated_at: '2026-03-26T10:00:00Z',
  tags: ['marketing'],
};

const sampleTag = {
  id: 1,
  name: 'marketing',
  created_at: '2026-03-26T10:00:00Z',
  link_count: 5,
};

const sampleAnalytics = {
  link_id: 1,
  total_clicks: 42,
  period_clicks: 15,
  clicks_by_day: [
    { date: '2026-03-25', count: 7 },
    { date: '2026-03-26', count: 8 },
  ],
};

export const handlers = [
  // Create link
  http.post(`${BASE_URL}/api/v1/links`, async ({ request }) => {
    const body = await request.json() as { url: string };
    if (!body.url || (!body.url.startsWith('http://') && !body.url.startsWith('https://'))) {
      return HttpResponse.json({ error: 'invalid URL: must be http or https' }, { status: 400 });
    }
    return HttpResponse.json({ ...sampleLink, original_url: body.url }, { status: 201 });
  }),

  // List links
  http.get(`${BASE_URL}/api/v1/links`, () => {
    return HttpResponse.json({
      links: [sampleLink],
      total: 1,
      page: 1,
      per_page: 20,
    });
  }),

  // Get single link
  http.get(`${BASE_URL}/api/v1/links/:id`, ({ params }) => {
    if (params.id === '999') {
      return HttpResponse.json({ error: 'not found' }, { status: 404 });
    }
    return HttpResponse.json(sampleLink);
  }),

  // Update link
  http.patch(`${BASE_URL}/api/v1/links/:id`, async ({ request }) => {
    const body = await request.json() as Record<string, unknown>;
    return HttpResponse.json({ ...sampleLink, ...body });
  }),

  // Delete link
  http.delete(`${BASE_URL}/api/v1/links/:id`, () => {
    return new HttpResponse(null, { status: 204 });
  }),

  // Analytics
  http.get(`${BASE_URL}/api/v1/links/:id/analytics`, ({ request }) => {
    const url = new URL(request.url);
    const period = url.searchParams.get('period');
    if (period === '24h') {
      return HttpResponse.json({
        link_id: 1,
        total_clicks: 42,
        period_clicks: 8,
        clicks_by_hour: [
          { hour: '2026-03-26T14:00:00Z', count: 4 },
          { hour: '2026-03-26T15:00:00Z', count: 4 },
        ],
      });
    }
    return HttpResponse.json(sampleAnalytics);
  }),

  // QR code
  http.get(`${BASE_URL}/api/v1/links/:id/qr`, () => {
    return new HttpResponse(new Blob(['fake-png']), {
      headers: { 'Content-Type': 'image/png' },
    });
  }),

  // Bulk create
  http.post(`${BASE_URL}/api/v1/links/bulk`, async ({ request }) => {
    const body = await request.json() as { urls: { url: string }[] };
    const results = body.urls.map((item, index) => {
      if (!item.url.startsWith('http')) {
        return { ok: false, error: 'invalid URL: must be http or https', index };
      }
      return { ok: true, link: { ...sampleLink, id: index + 1, original_url: item.url } };
    });
    return HttpResponse.json({
      total: body.urls.length,
      succeeded: results.filter(r => r.ok).length,
      failed: results.filter(r => !r.ok).length,
      results,
    });
  }),

  // Tags
  http.get(`${BASE_URL}/api/v1/tags`, () => {
    return HttpResponse.json({ tags: [sampleTag] });
  }),

  http.post(`${BASE_URL}/api/v1/tags`, async ({ request }) => {
    const body = await request.json() as { name: string };
    return HttpResponse.json({ id: 2, name: body.name, created_at: '2026-03-26T12:00:00Z' }, { status: 201 });
  }),

  http.delete(`${BASE_URL}/api/v1/tags/:id`, () => {
    return new HttpResponse(null, { status: 204 });
  }),
];

export const server = setupServer(...handlers);
```

#### Package.json Test Script

Add to `frontend/package.json`:

```json
{
  "scripts": {
    "test": "vitest run",
    "test:watch": "vitest",
    "test:coverage": "vitest run --coverage"
  }
}
```

#### Dependencies to Install

```bash
npm install -D vitest @testing-library/react @testing-library/jest-dom @testing-library/user-event msw jsdom @vitest/coverage-v8
```

---

### 2.2 Component Tests

#### ShortenForm (`frontend/src/components/ShortenForm.test.tsx`)

```typescript
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ShortenForm } from './ShortenForm';

describe('ShortenForm', () => {
  test('submits valid URL and shows result');
  // - Type URL, click submit
  // - Verify API called with correct payload
  // - Verify short URL displayed in result area

  test('shows error for invalid URL');
  // - Submit empty form → validation error displayed

  test('includes custom code when provided');
  // - Enter custom code, submit → API called with custom_code field

  test('includes expiration when selected');
  // - Select "1 hour" from dropdown → API called with expires_in=3600

  test('includes tags when selected');
  // - Select tags, submit → API called with tags array

  test('clears form after successful submission');
  // - Submit → form fields reset

  test('shows loading state during submission');
  // - Submit → button shows spinner/disabled
});
```

#### LinkTable (`frontend/src/components/LinkTable.test.tsx`)

```typescript
describe('LinkTable', () => {
  test('renders link rows with correct data');
  // - Renders short URL, original URL (truncated), created date, clicks, tags, status

  test('renders empty state when no links');
  // - No links → "No links yet" message

  test('renders pagination controls');
  // - total=25, per_page=10 → shows page numbers

  test('calls onPageChange when clicking pagination');

  test('renders action buttons for each row');
  // - Copy, QR, Analytics, Activate/Deactivate, Delete visible

  test('calls onDelete when delete button clicked');
  // - Click delete → confirmation → onDelete called with link ID

  test('calls onToggleActive when toggle button clicked');
  // - Active link → shows "Deactivate" button
  // - Inactive link → shows "Activate" button
});
```

#### CopyButton (`frontend/src/components/CopyButton.test.tsx`)

```typescript
describe('CopyButton', () => {
  test('copies text to clipboard on click');
  // - Click → navigator.clipboard.writeText called with URL

  test('shows feedback after copying');
  // - Click → button text changes to "Copied!" temporarily

  test('reverts to original state after timeout');
  // - Wait → button text returns to "Copy"
});
```

#### AnalyticsPanel (`frontend/src/components/AnalyticsPanel.test.tsx`)

```typescript
describe('AnalyticsPanel', () => {
  test('loads and displays analytics for default period (7d)');
  // - Renders total_clicks, period_clicks, bar chart

  test('switches period when button clicked');
  // - Click "24h" → re-fetches, shows hourly data

  test('shows loading state while fetching');

  test('shows error state on API failure');

  test('calls onClose when close button clicked');

  test('renders bar chart with correct data');
  // - Recharts BarChart rendered with correct dataKey
});
```

#### BulkShortenModal (`frontend/src/components/BulkShortenModal.test.tsx`)

```typescript
describe('BulkShortenModal', () => {
  test('renders textarea and submit button when open');

  test('does not render when isOpen=false');

  test('submits URLs and shows results');
  // - Enter 3 URLs, click submit
  // - Verify results: 2 success + 1 failure
  // - Success items show short URLs
  // - Failure items show error messages

  test('shows URL count indicator');
  // - Enter 3 lines → shows "3 URLs"

  test('disables submit when > 50 URLs');

  test('disables submit when empty');

  test('calls onSuccess after closing results');

  test('calls onClose when cancel clicked');
});
```

#### QRCodeModal (`frontend/src/components/QRCodeModal.test.tsx`)

```typescript
describe('QRCodeModal', () => {
  test('loads and displays QR code image');
  // - Verify fetch called to QR endpoint
  // - Image element rendered

  test('shows short URL label');

  test('download button triggers download');
  // - Click download → anchor element created with correct href

  test('shows loading state while fetching');

  test('shows error on fetch failure');

  test('calls onClose when close clicked');

  test('does not render when isOpen=false');
});
```

#### TagManager (`frontend/src/components/TagManager.test.tsx`)

```typescript
describe('TagManager', () => {
  test('renders tag list with link counts');
  // - Each tag shows name and link_count

  test('creates new tag on form submit');
  // - Type name, click "Add Tag" → tag appears in list

  test('validates tag name format');
  // - Enter invalid name → error message shown

  test('shows error on duplicate tag name');
  // - MSW returns 409 → error displayed

  test('deletes tag on button click');
  // - Click delete → confirm → tag removed from list

  test('shows empty state when no tags');
});
```

#### TagFilter (`frontend/src/components/TagFilter.test.tsx`)

```typescript
describe('TagFilter', () => {
  test('renders dropdown with "All tags" and tag options');

  test('calls onChange with tag name when selected');

  test('calls onChange with null when "All tags" selected');

  test('shows tag link counts in options');
});
```

#### SearchBar (`frontend/src/components/SearchBar.test.tsx`)

```typescript
describe('SearchBar', () => {
  test('calls onSearch when typing in search input');
  // - Type "example" → onSearch called (debounced)

  test('calls onSortChange when sort dropdown changes');

  test('calls onFilterChange when active filter changes');
});
```

#### API Client (`frontend/src/api/client.test.ts`)

```typescript
describe('API Client', () => {
  test('includes Authorization header in requests');
  // - Set API key in localStorage
  // - Make request → verify header present

  test('handles 401 by clearing stored key');
  // - MSW returns 401 → verify localStorage cleared

  test('creates link with correct payload');

  test('lists links with query parameters');

  test('handles network errors gracefully');
});
```

#### Hooks (`frontend/src/hooks/useLinks.test.ts`, `useTags.test.ts`)

```typescript
// useLinks.test.ts
describe('useLinks', () => {
  test('fetches links on mount');
  test('refetches when params change');
  test('handles pagination');
  test('handles errors');
});

// useTags.test.ts
describe('useTags', () => {
  test('fetches tags on mount');
  test('creates tag and updates local state');
  test('deletes tag and updates local state');
  test('handles errors');
});
```

---

## Part 3: Coverage and CI

### Running Tests

```bash
# Backend (all tests with race detector)
cd backend && go test ./... -race -count=1

# Backend with coverage report
cd backend && go test ./... -race -coverprofile=coverage.out && go tool cover -func=coverage.out

# Frontend
cd frontend && npm test

# Frontend with coverage
cd frontend && npm run test:coverage

# Both (via Makefile)
make test
```

### Coverage Targets (S16.3)

- **Backend**: 80% line coverage minimum.
- **Frontend**: All user-facing flows covered (no numeric target, but aim for meaningful coverage of component rendering, user interactions, API calls, error states).

### Makefile Test Targets (verify from Phase 12/13)

```makefile
test-backend:
	cd backend && go test ./... -race

test-frontend:
	cd frontend && npm test

test: test-backend test-frontend
```

---

## Verification Checklist

1. **Backend unit tests**:
   - `cd backend && go test ./... -race -count=1` — all pass.
   - `cd backend && go test ./... -race -coverprofile=coverage.out && go tool cover -func=coverage.out` — >= 80% line coverage.

2. **Backend integration tests**:
   - End-to-end create → redirect → analytics flow works.
   - Bulk create with mixed results tested.
   - Tag lifecycle tested.
   - Expiration and deactivation tested.

3. **Frontend tests**:
   - `cd frontend && npm test` — all pass.
   - All components have test files.
   - MSW intercepts all API calls (no unhandled requests).

4. **Full suite**:
   - `make test` — both backend and frontend pass.

5. **Error paths covered**:
   - Every error status code from S13 (400, 401, 404, 409, 410, 413, 429, 500) has at least one test.
   - Frontend displays appropriate error messages for API failures.
