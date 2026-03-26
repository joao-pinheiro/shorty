# Phase 4: Link CRUD + Redirect — Implementation Plan

## Summary

Implement the core URL shortening flow: create links, redirect via short code, list/get/update/delete links, and the health endpoint. This phase wires together the store, shortcode, urlcheck, and handler layers. Click recording is deferred to Phase 5 — the redirect handler works but does not yet record clicks.

---

## Step 1: Store Methods — `backend/internal/store/sqlite.go`

Replace the stubs for link-related methods.

### `CreateLink`

```go
func (s *SQLiteStore) CreateLink(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
	query := `
		INSERT INTO links (code, original_url, expires_at)
		VALUES (?, ?, ?)
		RETURNING id, code, original_url, created_at, expires_at, is_active, click_count, updated_at`

	var link model.Link
	var expiresAtStr sql.NullString
	var isActive int

	err := s.writeDB.QueryRowContext(ctx, query, code, originalURL, expiresAt).Scan(
		&link.ID, &link.Code, &link.OriginalURL,
		&link.CreatedAt, &expiresAtStr, &isActive,
		&link.ClickCount, &link.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert link: %w", err)
	}

	link.IsActive = isActive == 1
	if expiresAtStr.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", expiresAtStr.String)
		link.ExpiresAt = &t
	}

	return &link, nil
}
```

**Note on time parsing**: SQLite stores `CURRENT_TIMESTAMP` as `"2006-01-02 15:04:05"` format. Use a helper to parse consistently:

```go
func parseSQLiteTime(s string) (time.Time, error) {
	// Try ISO 8601 first (for user-supplied values), then SQLite default format
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}

func scanNullableTime(ns sql.NullString) *time.Time {
	if !ns.Valid {
		return nil
	}
	t, err := parseSQLiteTime(ns.String)
	if err != nil {
		return nil
	}
	return &t
}
```

### `CodeExists`

Uses the read pool (S9.3).

```go
func (s *SQLiteStore) CodeExists(ctx context.Context, code string) (bool, error) {
	var exists int
	err := s.readDB.QueryRowContext(ctx,
		"SELECT 1 FROM links WHERE code = ?", code,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
```

### `GetLinkByCode`

Uses the read pool (S9.3 — redirect lookups use the read pool).

```go
func (s *SQLiteStore) GetLinkByCode(ctx context.Context, code string) (*model.Link, error) {
	query := `
		SELECT id, code, original_url, created_at, expires_at, is_active, click_count, updated_at
		FROM links WHERE code = ?`

	var link model.Link
	var expiresAt sql.NullString
	var isActive int

	err := s.readDB.QueryRowContext(ctx, query, code).Scan(
		&link.ID, &link.Code, &link.OriginalURL,
		&link.CreatedAt, &expiresAt, &isActive,
		&link.ClickCount, &link.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("get link by code: %w", err)
	}

	link.IsActive = isActive == 1
	link.ExpiresAt = scanNullableTime(expiresAt)
	return &link, nil
}
```

### `GetLinkByID`

Uses the read pool.

```go
func (s *SQLiteStore) GetLinkByID(ctx context.Context, id int64) (*model.Link, error) {
	query := `
		SELECT id, code, original_url, created_at, expires_at, is_active, click_count, updated_at
		FROM links WHERE id = ?`

	// Same scanning logic as GetLinkByCode
	// Returns nil, nil if not found
}
```

### `ListLinks`

Implements pagination, search, sort, active filter, and tag filter (S6.4).

```go
func (s *SQLiteStore) ListLinks(ctx context.Context, params ListParams) (*ListResult, error) {
	// 1. Validate sort column against whitelist (S8.3)
	sortColumn := "created_at"
	switch params.Sort {
	case "created_at", "click_count", "expires_at":
		sortColumn = params.Sort
	case "":
		sortColumn = "created_at"
	default:
		return nil, fmt.Errorf("invalid sort column")
	}

	// 2. Validate order direction against whitelist (S8.3)
	orderDir := "DESC"
	switch strings.ToLower(params.Order) {
	case "asc":
		orderDir = "ASC"
	case "desc", "":
		orderDir = "DESC"
	default:
		return nil, fmt.Errorf("invalid order direction")
	}

	// 3. Build WHERE clauses with parameterized values
	var conditions []string
	var args []interface{}

	if params.Search != "" {
		conditions = append(conditions, "(original_url LIKE ? OR code LIKE ?)")
		pattern := "%" + params.Search + "%"
		args = append(args, pattern, pattern)
	}

	if params.Active != nil {
		if *params.Active {
			conditions = append(conditions, "is_active = 1")
		} else {
			conditions = append(conditions, "is_active = 0")
		}
	}

	if params.Tag != "" {
		conditions = append(conditions,
			"id IN (SELECT lt.link_id FROM link_tags lt JOIN tags t ON lt.tag_id = t.id WHERE t.name = ?)")
		args = append(args, params.Tag)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// 4. Count total matching rows
	countQuery := "SELECT COUNT(*) FROM links " + whereClause
	var total int
	if err := s.readDB.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count links: %w", err)
	}

	// 5. Fetch page
	// Sort and order are validated above, safe to interpolate (S8.3)
	offset := (params.Page - 1) * params.PerPage
	dataQuery := fmt.Sprintf(
		"SELECT id, code, original_url, created_at, expires_at, is_active, click_count, updated_at FROM links %s ORDER BY %s %s LIMIT ? OFFSET ?",
		whereClause, sortColumn, orderDir,
	)
	dataArgs := append(args, params.PerPage, offset)

	rows, err := s.readDB.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()

	var links []model.Link
	for rows.Next() {
		var link model.Link
		var expiresAt sql.NullString
		var isActive int
		if err := rows.Scan(
			&link.ID, &link.Code, &link.OriginalURL,
			&link.CreatedAt, &expiresAt, &isActive,
			&link.ClickCount, &link.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		link.IsActive = isActive == 1
		link.ExpiresAt = scanNullableTime(expiresAt)
		links = append(links, link)
	}

	return &ListResult{Links: links, Total: total}, nil
}
```

### `UpdateLink`

Only `is_active`, `expires_at`, and `tags` are mutable (S6.6). Tags are handled separately via `SetLinkTags`. This method handles `is_active` and `expires_at`. Must set `updated_at = CURRENT_TIMESTAMP` explicitly (S6.6).

```go
func (s *SQLiteStore) UpdateLink(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
	// Build dynamic SET clause
	var setClauses []string
	var args []interface{}

	if isActive != nil {
		val := 0
		if *isActive {
			val = 1
		}
		setClauses = append(setClauses, "is_active = ?")
		args = append(args, val)
	}

	if expiresAt != nil {
		setClauses = append(setClauses, "expires_at = ?")
		args = append(args, *expiresAt)
	}

	// Always update updated_at (S6.6)
	setClauses = append(setClauses, "updated_at = CURRENT_TIMESTAMP")

	if len(setClauses) == 1 {
		// Only updated_at — still execute to bump the timestamp
	}

	query := fmt.Sprintf(
		"UPDATE links SET %s WHERE id = ?",
		strings.Join(setClauses, ", "),
	)
	args = append(args, id)

	result, err := s.writeDB.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update link: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, nil // not found
	}

	// Re-fetch the updated link
	return s.GetLinkByID(ctx, id)
}
```

**Note**: `GetLinkByID` after update uses the read pool. Due to WAL mode, the read may see stale data if there's propagation delay. To avoid this, use `s.writeDB` for the re-fetch:

```go
// After update, fetch from write connection to guarantee consistency
return s.getLinkByIDFromDB(ctx, s.writeDB, id)
```

Extract a private helper `getLinkByIDFromDB(ctx, db, id)` that accepts a `*sql.DB` parameter.

### `DeactivateExpiredLink`

Lazy deactivation on redirect (S6.1). Also updates `updated_at` (S6.6).

```go
func (s *SQLiteStore) DeactivateExpiredLink(ctx context.Context, id int64) error {
	_, err := s.writeDB.ExecContext(ctx,
		"UPDATE links SET is_active = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}
```

### `DeleteLink`

Hard delete with cascade (S6.7). Cascade handled by `ON DELETE CASCADE` foreign keys.

```go
func (s *SQLiteStore) DeleteLink(ctx context.Context, id int64) error {
	result, err := s.writeDB.ExecContext(ctx, "DELETE FROM links WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
```

Define a sentinel error:

```go
var ErrNotFound = fmt.Errorf("not found")
```

---

## Step 2: Link Handlers — `backend/internal/handler/links.go`

### Handler Struct

```go
package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"shorty/internal/config"
	"shorty/internal/model"
	"shorty/internal/shortcode"
	"shorty/internal/store"
	"shorty/internal/urlcheck"
)

type LinkHandler struct {
	store   store.Store
	config  *config.Config
}

func NewLinkHandler(s store.Store, cfg *config.Config) *LinkHandler {
	return &LinkHandler{store: s, config: cfg}
}
```

### Create Link — `POST /api/v1/links` (S6.2)

#### Request Struct

```go
type CreateLinkRequest struct {
	URL        string   `json:"url"`
	CustomCode string   `json:"custom_code,omitempty"`
	ExpiresIn  *int     `json:"expires_in,omitempty"`
	Tags       []string `json:"tags,omitempty"`
}
```

#### Handler

```go
func (h *LinkHandler) Create(c echo.Context) error {
	var req CreateLinkRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	// 1. Trim whitespace (S8.3)
	req.URL = strings.TrimSpace(req.URL)
	req.CustomCode = strings.TrimSpace(req.CustomCode)

	// 2. Validate URL (S7 via urlcheck)
	if err := urlcheck.Validate(req.URL); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// 3. Validate expires_in (S6.2)
	var expiresAt *string
	if req.ExpiresIn != nil {
		if *req.ExpiresIn <= 0 || *req.ExpiresIn > 31536000 {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "expires_in must be a positive integer, max 31536000 (365 days)",
			})
		}
		t := time.Now().UTC().Add(time.Duration(*req.ExpiresIn) * time.Second).Format(time.RFC3339)
		expiresAt = &t
	}

	// 4. Validate tag names (S6.2 — fail entire request if any invalid)
	tagRegex := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,50}$`)
	for _, tag := range req.Tags {
		if !tagRegex.MatchString(strings.TrimSpace(tag)) {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"error": "invalid tag name: must be 1-50 alphanumeric, dash, or underscore",
			})
		}
	}

	// 5. Determine short code
	var code string
	if req.CustomCode != "" {
		// Validate custom code (S4)
		if errMsg := shortcode.ValidateCustomCode(req.CustomCode); errMsg != "" {
			status := http.StatusBadRequest
			if errMsg == "code is reserved" {
				return c.JSON(status, map[string]string{"error": "code is reserved"})
			}
			return c.JSON(status, map[string]string{"error": errMsg})
		}

		// Check collision (S4)
		exists, err := h.store.CodeExists(c.Request().Context(), req.CustomCode)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		if exists {
			return c.JSON(http.StatusConflict, map[string]string{"error": "code already in use"})
		}
		code = req.CustomCode
	} else {
		// Generate unique code (S4)
		var err error
		code, err = shortcode.GenerateUnique(h.config.DefaultCodeLength, func(c string) (bool, error) {
			return h.store.CodeExists(c.Request().Context(), c)
		})
		// IMPORTANT: closure captures `c` (echo.Context) — rename to avoid shadowing
		// Fix: use a different variable name in the closure
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
	}
```

**Closure variable shadowing fix**: The `ExistsFunc` parameter name `c` would shadow the Echo context `c`. Fix:

```go
	} else {
		ctx := c.Request().Context()
		var err error
		code, err = shortcode.GenerateUnique(h.config.DefaultCodeLength, func(candidate string) (bool, error) {
			return h.store.CodeExists(ctx, candidate)
		})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
	}
```

Continue the handler:

```go
	// 6. Create link in store
	link, err := h.store.CreateLink(c.Request().Context(), code, req.URL, expiresAt)
	if err != nil {
		slog.Error("create link failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	// 7. Set tags if provided (Phase 6 will implement SetLinkTags)
	if len(req.Tags) > 0 {
		if err := h.store.SetLinkTags(c.Request().Context(), link.ID, req.Tags); err != nil {
			slog.Error("set link tags failed", "error", err)
			// Link was created — don't fail the whole request, but log the error
			// Actually per S6.2, if any tag name is invalid the entire request fails.
			// Tag name validation already passed above. This error would be a DB error.
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		link.Tags = req.Tags
	}

	// 8. Build response with short_url
	link.ShortURL = h.config.BaseURL + "/" + link.Code

	return c.JSON(http.StatusCreated, link)
}
```

### List Links — `GET /api/v1/links` (S6.4)

```go
func (h *LinkHandler) List(c echo.Context) error {
	// Parse query parameters with defaults
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	perPage, _ := strconv.Atoi(c.QueryParam("per_page"))
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}

	var active *bool
	if activeStr := c.QueryParam("active"); activeStr != "" {
		b := activeStr == "true"
		active = &b
	}

	params := store.ListParams{
		Page:    page,
		PerPage: perPage,
		Search:  strings.TrimSpace(c.QueryParam("search")),
		Sort:    c.QueryParam("sort"),
		Order:   c.QueryParam("order"),
		Active:  active,
		Tag:     strings.TrimSpace(c.QueryParam("tag")),
	}

	result, err := h.store.ListLinks(c.Request().Context(), params)
	if err != nil {
		slog.Error("list links failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	// Build short_url for each link
	for i := range result.Links {
		result.Links[i].ShortURL = h.config.BaseURL + "/" + result.Links[i].Code
	}

	// Batch-fetch tags for all links (Phase 6)
	// For now, Tags will be nil/empty

	return c.JSON(http.StatusOK, map[string]interface{}{
		"links":    result.Links,
		"total":    result.Total,
		"page":     page,
		"per_page": perPage,
	})
}
```

### Get Single Link — `GET /api/v1/links/:id` (S6.5)

```go
func (h *LinkHandler) Get(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	link, err := h.store.GetLinkByID(c.Request().Context(), id)
	if err != nil {
		slog.Error("get link failed", "error", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	if link == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}

	link.ShortURL = h.config.BaseURL + "/" + link.Code

	// Fetch tags (Phase 6)
	tags, err := h.store.GetLinkTags(c.Request().Context(), link.ID)
	if err == nil {
		link.Tags = tags
	}

	// S6.5: expired links returned normally with is_active:false
	return c.JSON(http.StatusOK, link)
}
```

### Update Link — `PATCH /api/v1/links/:id` (S6.6)

#### Request Struct

```go
type UpdateLinkRequest struct {
	IsActive  *bool    `json:"is_active,omitempty"`
	ExpiresAt *string  `json:"expires_at,omitempty"` // RFC3339 string
	Tags      *[]string `json:"tags,omitempty"`       // pointer to distinguish absent from empty array
}
```

**Note on `Tags`**: Use `*[]string` so JSON `"tags": []` (empty array = remove all tags) is distinguishable from `"tags"` being absent (don't touch tags). `nil` pointer means field was not in request body.

```go
func (h *LinkHandler) Update(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	var req UpdateLinkRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}

	// Check link exists
	existing, err := h.store.GetLinkByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	if existing == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}

	// Validate tag names if provided
	if req.Tags != nil {
		tagRegex := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,50}$`)
		for _, tag := range *req.Tags {
			if !tagRegex.MatchString(strings.TrimSpace(tag)) {
				return c.JSON(http.StatusBadRequest, map[string]string{
					"error": "invalid tag name: must be 1-50 alphanumeric, dash, or underscore",
				})
			}
		}
	}

	// Update link fields
	link, err := h.store.UpdateLink(c.Request().Context(), id, req.IsActive, req.ExpiresAt)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	if link == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}

	// Update tags if provided (full replacement, S6.6)
	if req.Tags != nil {
		if err := h.store.SetLinkTags(c.Request().Context(), id, *req.Tags); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		link.Tags = *req.Tags
	} else {
		// Fetch existing tags
		tags, _ := h.store.GetLinkTags(c.Request().Context(), id)
		link.Tags = tags
	}

	link.ShortURL = h.config.BaseURL + "/" + link.Code
	return c.JSON(http.StatusOK, link)
}
```

### Delete Link — `DELETE /api/v1/links/:id` (S6.7)

```go
func (h *LinkHandler) Delete(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid id"})
	}

	err = h.store.DeleteLink(c.Request().Context(), id)
	if err == store.ErrNotFound {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	return c.NoContent(http.StatusNoContent)
}
```

---

## Step 3: Redirect Handler — `backend/internal/handler/redirect.go`

```go
package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"shorty/internal/store"
)

type RedirectHandler struct {
	store  store.Store
	// clickCh will be added in Phase 5
}

func NewRedirectHandler(s store.Store) *RedirectHandler {
	return &RedirectHandler{store: s}
}

func (h *RedirectHandler) Redirect(c echo.Context) error {
	code := c.Param("code")

	// Strip trailing slash (S19: edge case)
	code = strings.TrimSuffix(code, "/")

	if code == "" {
		// Fall through to SPA — this will be handled by routing middleware in Phase 12
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}

	link, err := h.store.GetLinkByCode(c.Request().Context(), code)
	if err != nil {
		slog.Error("redirect lookup failed", "error", err, "code", code)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	// Not found (S6.1)
	if link == nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
	}

	// Deactivated (S6.1)
	if !link.IsActive {
		return c.JSON(http.StatusGone, map[string]string{"error": "link is deactivated"})
	}

	// Expired — lazy deactivation (S6.1)
	if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now().UTC()) {
		// Lazily set is_active = 0 with updated_at (S6.6)
		if err := h.store.DeactivateExpiredLink(c.Request().Context(), link.ID); err != nil {
			slog.Error("lazy deactivation failed", "error", err, "link_id", link.ID)
		}
		return c.JSON(http.StatusGone, map[string]string{"error": "link has expired"})
	}

	// Record click — Phase 5 will add: h.clickCh <- store.ClickEvent{LinkID: link.ID}

	// Security headers for redirect (S8.5)
	c.Response().Header().Set("X-Frame-Options", "DENY")

	// Cache-Control (S6.1)
	c.Response().Header().Set("Cache-Control", "private, max-age=0")

	// 302 temporary redirect (S6.1)
	return c.Redirect(http.StatusFound, link.OriginalURL)
}
```

---

## Step 4: Health Handler — `backend/internal/handler/health.go`

```go
package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func HealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status":  "ok",
		"version": "1.0.0",
	})
}
```

---

## Step 5: Route Registration — Updated `backend/cmd/shorty/main.go`

```go
// Create handlers
linkHandler := handler.NewLinkHandler(db, cfg)
redirectHandler := handler.NewRedirectHandler(db)

// Public routes (no auth)
e.GET("/api/health", handler.HealthCheck)

// API v1 routes (auth required)
apiV1 := e.Group("/api/v1", handler.AuthMiddleware(cfg.APIKey))

apiV1.POST("/links", linkHandler.Create,
	handler.RateLimitMiddleware(createLinkLimiter, cfg.RateLimitEnabled))
apiV1.GET("/links", linkHandler.List,
	handler.RateLimitMiddleware(defaultLimiter, cfg.RateLimitEnabled))
apiV1.GET("/links/:id", linkHandler.Get,
	handler.RateLimitMiddleware(defaultLimiter, cfg.RateLimitEnabled))
apiV1.PATCH("/links/:id", linkHandler.Update,
	handler.RateLimitMiddleware(defaultLimiter, cfg.RateLimitEnabled))
apiV1.DELETE("/links/:id", linkHandler.Delete,
	handler.RateLimitMiddleware(defaultLimiter, cfg.RateLimitEnabled))

// Redirect route (no auth, rate limited)
// NOTE: This must be registered after /api/* routes to avoid conflicts.
// In Phase 12, this moves into the catch-all middleware.
// For now, register as an Echo route:
e.GET("/:code", redirectHandler.Redirect,
	handler.RateLimitMiddleware(redirectLimiter, cfg.RateLimitEnabled))
```

---

## Step 6: Link Response JSON Tags

Ensure the `model.Link` struct produces correct JSON. Key details:

- `short_url` is computed, not stored — set it before returning.
- `expires_at` must serialize as `null` (not omitted) when nil. Use `*time.Time` with `json:"expires_at"` (no `omitempty`).
- `is_active` is a bool in JSON, not an int.
- `tags` should be `[]string` (empty array `[]`, not `null`, when no tags). Initialize to `[]string{}` if nil:

```go
if link.Tags == nil {
	link.Tags = []string{}
}
```

Verify JSON output matches S6.2 response format exactly.

---

## Error Handling Summary

| Condition | Status | Error Message | Spec |
|-----------|--------|---------------|------|
| Invalid/missing URL | 400 | `"invalid URL: must be http or https"` | S6.2 |
| URL > 2048 chars | 400 | `"URL exceeds 2048 characters"` | S6.2 |
| Custom code taken | 409 | `"code already in use"` | S6.2 |
| Custom code bad format | 400 | `"code must be 3-32 alphanumeric, dash, or underscore"` | S6.2 |
| Custom code reserved | 400 | `"code is reserved"` | S6.2 |
| SSRF/unsafe URL | 400 | `"URL flagged as potentially unsafe"` | S6.2 |
| Invalid tag name | 400 | `"invalid tag name: must be 1-50 alphanumeric, dash, or underscore"` | S6.2 |
| Invalid expires_in | 400 | `"expires_in must be a positive integer, max 31536000 (365 days)"` | S6.2 |
| Redirect not found | 404 | `"not found"` | S6.1 |
| Redirect deactivated | 410 | `"link is deactivated"` | S6.1 |
| Redirect expired | 410 | `"link has expired"` | S6.1 |
| Resource not found | 404 | `"not found"` | S6.5, S6.7 |

---

## Verification Checklist

1. **Create link**:
   ```bash
   curl -X POST http://localhost:8080/api/v1/links \
     -H "Authorization: Bearer <key>" \
     -H "Content-Type: application/json" \
     -d '{"url": "https://example.com/test"}'
   ```
   Expect: `201` with `id`, `code` (6 chars), `short_url`, `original_url`, `created_at`, `expires_at: null`, `is_active: true`, `click_count: 0`, `updated_at`, `tags: []`.

2. **Create with custom code**:
   ```bash
   curl -X POST ... -d '{"url": "https://example.com", "custom_code": "my-link"}'
   ```
   Expect: `201` with `code: "my-link"`.

3. **Duplicate custom code**: Same request again → `409 {"error": "code already in use"}`.

4. **Invalid URL**: `{"url": "ftp://bad"}` → `400 {"error": "invalid URL: must be http or https"}`.

5. **Private IP**: `{"url": "http://192.168.1.1"}` → `400 {"error": "URL flagged as potentially unsafe"}`.

6. **Redirect**: `curl -v http://localhost:8080/<code>` → `302` with `Location: https://example.com/test`, `Cache-Control: private, max-age=0`, `X-Frame-Options: DENY`.

7. **Redirect not found**: `curl http://localhost:8080/nonexistent` → `404 {"error": "not found"}`.

8. **List links**: `curl -H "Authorization: ..." http://localhost:8080/api/v1/links?page=1&per_page=5` → paginated response with `links`, `total`, `page`, `per_page`.

9. **Search**: `?search=example` → filters results.

10. **Update link**: `PATCH /api/v1/links/1` with `{"is_active": false}` → `200` with `is_active: false`.

11. **Redirect deactivated**: Visit the short URL → `410 {"error": "link is deactivated"}`.

12. **Delete link**: `DELETE /api/v1/links/1` → `204` No Content.

13. **Delete nonexistent**: `DELETE /api/v1/links/999` → `404 {"error": "not found"}`.

14. **Expiration**: Create with `{"url": "...", "expires_in": 2}`, wait 3 seconds, redirect → `410 {"error": "link has expired"}`.

15. **Get expired via API**: `GET /api/v1/links/1` → still returns the link with `is_active: false` (S6.5).
