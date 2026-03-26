# Phase 7: Bulk Create, Analytics, QR

## Summary

Implement the three remaining API endpoints: bulk link creation (`POST /api/v1/links/bulk`), click analytics (`GET /api/v1/links/:id/analytics`), and QR code generation (`GET /api/v1/links/:id/qr` + public `GET /:code/qr`). Bulk create processes each URL independently, returning per-item results. Analytics supports four period values with day or hour bucketing. QR codes are generated on the fly as PNG images.

Spec references: S6.3 (bulk create), S6.8 (analytics), S6.9 (QR code), S8.1 (rate limits for bulk and public QR), S15.5 (public QR routing).

---

## Step 1: Bulk Create — Store Layer

No new store methods needed. Bulk create reuses the existing `CreateLink` and `SetLinkTags` from Phases 4 and 6. Each item is processed independently with its own store call.

---

## Step 2: Bulk Create — Handler (`backend/internal/handler/bulk.go`)

### Request/Response Types

```go
type bulkCreateRequest struct {
    URLs []bulkCreateItem `json:"urls"`
}

type bulkCreateItem struct {
    URL        string   `json:"url"`
    CustomCode string   `json:"custom_code"`
    ExpiresIn  *int     `json:"expires_in"`
    Tags       []string `json:"tags"`
}

type bulkResultItem struct {
    OK    bool        `json:"ok"`
    Link  interface{} `json:"link,omitempty"`  // *linkResponse when ok=true
    Error string      `json:"error,omitempty"` // error message when ok=false
    Index *int        `json:"index,omitempty"` // 0-based index, only on errors
}

type bulkCreateResponse struct {
    Total     int              `json:"total"`
    Succeeded int              `json:"succeeded"`
    Failed    int              `json:"failed"`
    Results   []bulkResultItem `json:"results"`
}
```

### Handler Struct

```go
type BulkHandler struct {
    store     store.Store
    cfg       *config.Config
    // Reuses validation logic from LinkHandler — either embed LinkHandler
    // or extract shared validation functions into a helper.
}

func NewBulkHandler(s store.Store, cfg *config.Config) *BulkHandler {
    return &BulkHandler{store: s, cfg: cfg}
}
```

### `POST /api/v1/links/bulk` (S6.3)

```go
func (h *BulkHandler) Create(c echo.Context) error {
    var req bulkCreateRequest
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, errorResponse("invalid request body"))
    }

    // Enforce max 50 URLs (S6.3), configurable via MAX_BULK_URLS (S11)
    if len(req.URLs) == 0 {
        return c.JSON(http.StatusBadRequest, errorResponse("urls array is required and must not be empty"))
    }
    if len(req.URLs) > h.cfg.MaxBulkURLs {
        return c.JSON(http.StatusBadRequest, errorResponse(
            fmt.Sprintf("maximum %d URLs per request", h.cfg.MaxBulkURLs)))
    }

    ctx := c.Request().Context()
    results := make([]bulkResultItem, len(req.URLs))
    succeeded := 0
    failed := 0

    for i, item := range req.URLs {
        link, tags, err := h.processOneLink(ctx, item)
        if err != nil {
            idx := i
            results[i] = bulkResultItem{OK: false, Error: err.Error(), Index: &idx}
            failed++
        } else {
            results[i] = bulkResultItem{
                OK:   true,
                Link: buildLinkResponse(link, tags, h.cfg.BaseURL),
            }
            succeeded++
        }
    }

    return c.JSON(http.StatusOK, bulkCreateResponse{
        Total:     len(req.URLs),
        Succeeded: succeeded,
        Failed:    failed,
        Results:   results,
    })
}
```

### `processOneLink` — Shared Validation Logic

Extract the validation and creation logic used by the single-create endpoint into a reusable function. Both `POST /api/v1/links` and each bulk item call this:

```go
func (h *BulkHandler) processOneLink(ctx context.Context, item bulkCreateItem) (model.Link, []string, error) {
    // 1. Trim and validate URL (urlcheck.Validate)
    // 2. Validate custom code format, reserved words, collision
    // 3. Validate expires_in (positive, max 31536000)
    // 4. Validate tag names (^[a-zA-Z0-9_-]{1,50}$)
    // 5. Generate code if no custom_code
    // 6. Create link in store
    // 7. Set link tags if present
    // Returns the created link, tag names, or an error with user-facing message
}
```

The error messages returned must match the exact strings from S6.2 (e.g., `"invalid URL: must be http or https"`, `"code already in use"`, etc.) since they appear directly in the bulk response.

### Rate Limiting

The bulk endpoint has its own rate limit: 2/min with burst 5 (S8.1). This is configured in the rate limit middleware from Phase 3 by matching the route path.

---

## Step 3: Analytics — Store Methods

Add to `Store` interface:

```go
GetClicksByDay(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error)
GetClicksByHour(ctx context.Context, linkID int64, since time.Time) ([]model.HourCount, error)
GetPeriodClickCount(ctx context.Context, linkID int64, since time.Time) (int, error)
GetTotalClickCount(ctx context.Context, linkID int64) (int, error)
```

### Model Types

Add to `backend/internal/model/model.go`:

```go
type DayCount struct {
    Date  string `json:"date"`  // "2026-03-25"
    Count int    `json:"count"`
}

type HourCount struct {
    Hour  string `json:"hour"`  // "2026-03-26T14:00:00Z"
    Count int    `json:"count"`
}
```

### SQLite Implementation

#### `GetClicksByDay`

```go
func (s *SQLiteStore) GetClicksByDay(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error) {
    query := `SELECT strftime('%Y-%m-%d', clicked_at) AS date, COUNT(*) AS count
              FROM clicks
              WHERE link_id = ? AND clicked_at >= ?
              GROUP BY date
              ORDER BY date ASC`
    rows, err := s.readDB.QueryContext(ctx, query, linkID, since.UTC().Format(time.RFC3339))
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var results []model.DayCount
    for rows.Next() {
        var dc model.DayCount
        if err := rows.Scan(&dc.Date, &dc.Count); err != nil {
            return nil, err
        }
        results = append(results, dc)
    }
    return results, rows.Err()
}
```

#### `GetClicksByHour`

```go
func (s *SQLiteStore) GetClicksByHour(ctx context.Context, linkID int64, since time.Time) ([]model.HourCount, error) {
    query := `SELECT strftime('%Y-%m-%dT%H:00:00Z', clicked_at) AS hour, COUNT(*) AS count
              FROM clicks
              WHERE link_id = ? AND clicked_at >= ?
              GROUP BY hour
              ORDER BY hour ASC`
    rows, err := s.readDB.QueryContext(ctx, query, linkID, since.UTC().Format(time.RFC3339))
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var results []model.HourCount
    for rows.Next() {
        var hc model.HourCount
        if err := rows.Scan(&hc.Hour, &hc.Count); err != nil {
            return nil, err
        }
        results = append(results, hc)
    }
    return results, rows.Err()
}
```

#### `GetPeriodClickCount`

```go
func (s *SQLiteStore) GetPeriodClickCount(ctx context.Context, linkID int64, since time.Time) (int, error) {
    var count int
    err := s.readDB.QueryRowContext(ctx,
        `SELECT COUNT(*) FROM clicks WHERE link_id = ? AND clicked_at >= ?`,
        linkID, since.UTC().Format(time.RFC3339)).Scan(&count)
    return count, err
}
```

#### `GetTotalClickCount`

```go
func (s *SQLiteStore) GetTotalClickCount(ctx context.Context, linkID int64) (int, error) {
    var count int
    err := s.readDB.QueryRowContext(ctx,
        `SELECT click_count FROM links WHERE id = ?`, linkID).Scan(&count)
    return count, err
}
```

---

## Step 4: Analytics — Handler (`backend/internal/handler/analytics.go`)

### Handler Struct

```go
type AnalyticsHandler struct {
    store store.Store
}

func NewAnalyticsHandler(s store.Store) *AnalyticsHandler {
    return &AnalyticsHandler{store: s}
}
```

### `GET /api/v1/links/:id/analytics` (S6.8)

```go
// Allowed period values — whitelist for query interpolation safety (S8.3)
var allowedPeriods = map[string]bool{
    "24h": true,
    "7d":  true,
    "30d": true,
    "all": true,
}

func (h *AnalyticsHandler) Get(c echo.Context) error {
    id, err := strconv.ParseInt(c.Param("id"), 10, 64)
    if err != nil {
        return c.JSON(http.StatusBadRequest, errorResponse("invalid link ID"))
    }

    period := c.QueryParam("period")
    if period == "" {
        period = "7d" // sensible default
    }
    if !allowedPeriods[period] {
        return c.JSON(http.StatusBadRequest, errorResponse(
            "period must be one of: 24h, 7d, 30d, all"))
    }

    ctx := c.Request().Context()

    // Verify link exists
    _, err = h.store.GetLinkByID(ctx, id)
    if err != nil {
        if errors.Is(err, store.ErrLinkNotFound) {
            return c.JSON(http.StatusNotFound, errorResponse("not found"))
        }
        return internalError(c, err)
    }

    // Get total clicks (from links.click_count)
    totalClicks, err := h.store.GetTotalClickCount(ctx, id)
    if err != nil {
        return internalError(c, err)
    }

    // Calculate since time
    now := time.Now().UTC()
    var since time.Time
    switch period {
    case "24h":
        since = now.Add(-24 * time.Hour)
    case "7d":
        since = now.AddDate(0, 0, -7)
    case "30d":
        since = now.AddDate(0, 0, -30)
    case "all":
        since = time.Time{} // zero time = no lower bound
    }

    // Get period click count
    var periodClicks int
    if period == "all" {
        periodClicks = totalClicks
    } else {
        periodClicks, err = h.store.GetPeriodClickCount(ctx, id, since)
        if err != nil {
            return internalError(c, err)
        }
    }

    // Build response based on period
    response := map[string]interface{}{
        "link_id":      id,
        "total_clicks": totalClicks,
        "period_clicks": periodClicks,
    }

    if period == "24h" {
        // Return clicks_by_hour (S6.8 — field is absent, not null, for non-24h)
        hours, err := h.store.GetClicksByHour(ctx, id, since)
        if err != nil {
            return internalError(c, err)
        }
        if hours == nil {
            hours = []model.HourCount{}
        }
        response["clicks_by_hour"] = hours
    } else {
        // Return clicks_by_day
        days, err := h.store.GetClicksByDay(ctx, id, since)
        if err != nil {
            return internalError(c, err)
        }
        if days == nil {
            days = []model.DayCount{}
        }
        response["clicks_by_day"] = days
    }

    return c.JSON(http.StatusOK, response)
}
```

**Response shapes** per S6.8:

For `7d`, `30d`, `all`:
```json
{
  "link_id": 1,
  "total_clicks": 1523,
  "period_clicks": 342,
  "clicks_by_day": [{"date": "2026-03-25", "count": 45}]
}
```

For `24h`:
```json
{
  "link_id": 1,
  "total_clicks": 1523,
  "period_clicks": 87,
  "clicks_by_hour": [{"hour": "2026-03-26T14:00:00Z", "count": 12}]
}
```

---

## Step 5: QR Code — Package (`backend/internal/qr/qr.go`)

### Function Signature

```go
package qr

import qrcode "github.com/skip2/go-qrcode"

// Generate creates a QR code PNG for the given URL at the specified pixel size.
// size must be between 128 and 1024 (inclusive).
func Generate(url string, size int) ([]byte, error) {
    if size < 128 {
        size = 128
    }
    if size > 1024 {
        size = 1024
    }
    return qrcode.Encode(url, qrcode.Medium, size)
}
```

Uses `github.com/skip2/go-qrcode` (S18). Error correction level Medium is a reasonable default for URL QR codes.

---

## Step 6: QR Code — Authenticated Handler

Add to the link handler or a separate file. Route: `GET /api/v1/links/:id/qr` (S6.9).

```go
func (h *LinkHandler) QRCode(c echo.Context) error {
    id, err := strconv.ParseInt(c.Param("id"), 10, 64)
    if err != nil {
        return c.JSON(http.StatusBadRequest, errorResponse("invalid link ID"))
    }

    ctx := c.Request().Context()
    link, err := h.store.GetLinkByID(ctx, id)
    if err != nil {
        if errors.Is(err, store.ErrLinkNotFound) {
            return c.JSON(http.StatusNotFound, errorResponse("not found"))
        }
        return internalError(c, err)
    }

    size := 256 // default
    if s := c.QueryParam("size"); s != "" {
        parsed, err := strconv.Atoi(s)
        if err == nil {
            size = parsed
        }
    }
    // Clamp to [128, 1024]
    if size < 128 {
        size = 128
    }
    if size > 1024 {
        size = 1024
    }

    shortURL := h.cfg.BaseURL + "/" + link.Code
    png, err := qr.Generate(shortURL, size)
    if err != nil {
        return internalError(c, err)
    }

    return c.Blob(http.StatusOK, "image/png", png)
}
```

### Public QR Route

The public route `GET /:code/qr` does NOT require auth and does NOT check link status — it always returns the QR code if the code exists (S6.9). This will be wired into the catch-all routing middleware in Phase 12. However, the QR generation function (`qr.Generate`) is ready now.

For Phase 12 routing middleware, the logic will be:

```go
// In the catch-all middleware (Phase 12):
// Check if path matches /:code/qr
if strings.HasSuffix(path, "/qr") {
    code := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/qr")
    if code != "" {
        link, err := store.GetLinkByCode(ctx, code)
        if err == nil {
            shortURL := cfg.BaseURL + "/" + link.Code
            size := parseSizeParam(c, 256)
            png, _ := qr.Generate(shortURL, size)
            return c.Blob(200, "image/png", png)
        }
    }
}
```

---

## Step 7: Route Registration

In `main.go`:

```go
bulkHandler := handler.NewBulkHandler(store, cfg)
analyticsHandler := handler.NewAnalyticsHandler(store)

apiV1 := e.Group("/api/v1", authMiddleware)

// Bulk — with its own rate limit (2/min, burst 5)
apiV1.POST("/links/bulk", bulkHandler.Create)

// Analytics
apiV1.GET("/links/:id/analytics", analyticsHandler.Get)

// QR (authenticated)
apiV1.GET("/links/:id/qr", linkHandler.QRCode)
```

Rate limits per S8.1:
- `POST /api/v1/links/bulk`: 2/min, burst 5
- Other API endpoints (analytics, QR): 30/min, burst 60

---

## Step 8: Error Handling

### Bulk Create Errors

The overall request returns 200 even when individual items fail (S6.3). Per-item errors use the same messages as S6.2. Only return non-200 for:

| Condition | Status | Error |
|-----------|--------|-------|
| Empty `urls` array | 400 | `"urls array is required and must not be empty"` |
| More than MAX_BULK_URLS items | 400 | `"maximum 50 URLs per request"` |
| Invalid JSON body | 400 | `"invalid request body"` |

### Analytics Errors

| Condition | Status | Error |
|-----------|--------|-------|
| Invalid link ID | 400 | `"invalid link ID"` |
| Link not found | 404 | `"not found"` |
| Invalid period value | 400 | `"period must be one of: 24h, 7d, 30d, all"` |

### QR Errors

| Condition | Status | Error |
|-----------|--------|-------|
| Invalid link ID | 400 | `"invalid link ID"` |
| Link not found | 404 | `"not found"` |

---

## Step 9: Testing

### Store Tests

1. **GetClicksByDay**: Insert clicks on different days, query with since, verify grouped results. Verify empty result when no clicks.
2. **GetClicksByHour**: Insert clicks at different hours within a day, verify hourly buckets.
3. **GetPeriodClickCount**: Insert clicks spanning a range, verify count with different since values.
4. **GetTotalClickCount**: Create link, increment click_count, verify.

### Handler Tests — Bulk

Using mock store:

1. **Happy path**: 3 valid URLs, all succeed, verify response shape with `total: 3, succeeded: 3, failed: 0`.
2. **Mixed results**: 3 URLs where 1 has invalid URL, verify `succeeded: 2, failed: 1`, error item has correct `index` and `error` message.
3. **Custom code collision**: One item with taken custom code, verify `ok: false` with `"code already in use"`.
4. **Max limit exceeded**: 51 URLs, verify 400.
5. **Empty array**: Verify 400.
6. **Tags in bulk items**: Verify tags are associated with created links.

### Handler Tests — Analytics

1. **7d period**: Verify `clicks_by_day` present, `clicks_by_hour` absent.
2. **24h period**: Verify `clicks_by_hour` present, `clicks_by_day` absent.
3. **all period**: Verify `period_clicks` equals `total_clicks`.
4. **Link not found**: Verify 404.
5. **Invalid period**: Verify 400.

### Handler Tests — QR

1. **Valid request**: Verify 200, `Content-Type: image/png`, non-empty body.
2. **Custom size**: `?size=512`, verify response.
3. **Size clamping**: `?size=50` uses 128, `?size=2000` uses 1024.
4. **Link not found**: Verify 404.

### QR Package Tests

1. **Generate**: Verify returns valid PNG bytes (check PNG header `\x89PNG`).
2. **Size bounds**: Pass 100, verify no error (clamped to 128). Pass 2000, verify no error (clamped to 1024).

### Integration Tests

1. Bulk create 3 URLs (1 invalid), follow valid short URLs to verify redirects work.
2. Create link, generate clicks via redirect, wait for flush, query analytics, verify counts match.
3. Fetch QR for authenticated route, verify PNG.

### Verification Commands

```bash
cd backend && go test ./internal/handler/... -run TestBulk -race -v
cd backend && go test ./internal/handler/... -run TestAnalytics -race -v
cd backend && go test ./internal/qr/... -race -v
cd backend && go test ./... -race -count=1
```
