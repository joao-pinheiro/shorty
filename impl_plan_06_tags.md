# Phase 6: Tags

## Summary

Implement tag CRUD endpoints (`GET /api/v1/tags`, `POST /api/v1/tags`, `DELETE /api/v1/tags/:id`) and integrate tags into existing link endpoints. Tags are associated with links via the `link_tags` join table. Creating a link or updating a link can reference tags by name; tags are auto-created if they don't exist. A maximum of 100 tags is enforced globally. The list endpoint gains a `tag` query filter.

Spec references: S3.1 (link_tags, tags tables), S6.2 (create link with tags), S6.4 (list links with tag filter), S6.6 (update link tags — full replacement), S6.10 (list tags), S6.11 (create tag), S6.12 (delete tag), S19 (tag deletion cascades to link_tags).

---

## Step 1: Store Interface — Add Tag Methods

Add these method signatures to `Store` interface in `backend/internal/store/store.go`:

```go
// Tag CRUD
CreateTag(ctx context.Context, name string) (model.Tag, error)
ListTags(ctx context.Context) ([]model.TagWithCount, error)
GetTagByID(ctx context.Context, id int64) (model.Tag, error)
DeleteTag(ctx context.Context, id int64) error
TagCount(ctx context.Context) (int, error)

// Link-tag associations
SetLinkTags(ctx context.Context, linkID int64, tagNames []string) error
GetLinkTags(ctx context.Context, linkID int64) ([]string, error)
GetLinksTagsBatch(ctx context.Context, linkIDs []int64) (map[int64][]string, error)
```

---

## Step 2: Model Definitions

Add to `backend/internal/model/model.go`:

```go
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

These may already exist from Phase 1 model stubs. Verify and add `TagWithCount` if missing.

---

## Step 3: Store Implementation — `sqlite.go`

### 3.1 `CreateTag`

```go
func (s *SQLiteStore) CreateTag(ctx context.Context, name string) (*model.Tag, error) {
    res, err := s.writeDB.ExecContext(ctx,
        `INSERT INTO tags (name) VALUES (?)`, name)
    if err != nil {
        // Check for UNIQUE constraint violation → return sentinel ErrTagExists
        if isUniqueConstraintError(err) {
            return nil, ErrTagExists
        }
        return nil, err
    }
    id, _ := res.LastInsertId()
    var tag model.Tag
    err = s.readDB.QueryRowContext(ctx,
        `SELECT id, name, created_at FROM tags WHERE id = ?`, id).
        Scan(&tag.ID, &tag.Name, &tag.CreatedAt)
    if err != nil {
        return nil, err
    }
    return &tag, nil
}
```

Define sentinel errors:

```go
var (
    ErrTagExists   = errors.New("tag already exists")
    ErrTagNotFound = errors.New("tag not found")
)
```

### 3.2 `ListTags`

```sql
SELECT t.id, t.name, t.created_at, COUNT(lt.link_id) AS link_count
FROM tags t
LEFT JOIN link_tags lt ON lt.tag_id = t.id
GROUP BY t.id
ORDER BY t.name ASC
```

```go
func (s *SQLiteStore) ListTags(ctx context.Context) ([]model.TagWithCount, error) {
    rows, err := s.readDB.QueryContext(ctx, /* above SQL */)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var tags []model.TagWithCount
    for rows.Next() {
        var t model.TagWithCount
        if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.LinkCount); err != nil {
            return nil, err
        }
        tags = append(tags, t)
    }
    return tags, rows.Err()
}
```

### 3.3 `GetTagByID`

```go
func (s *SQLiteStore) GetTagByID(ctx context.Context, id int64) (*model.Tag, error) {
    var tag model.Tag
    err := s.readDB.QueryRowContext(ctx,
        `SELECT id, name, created_at FROM tags WHERE id = ?`, id).
        Scan(&tag.ID, &tag.Name, &tag.CreatedAt)
    if err == sql.ErrNoRows {
        return nil, ErrTagNotFound
    }
    if err != nil {
        return nil, err
    }
    return &tag, nil
}
```

### 3.4 `DeleteTag`

```go
func (s *SQLiteStore) DeleteTag(ctx context.Context, id int64) error {
    res, err := s.writeDB.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
    if err != nil {
        return err
    }
    n, _ := res.RowsAffected()
    if n == 0 {
        return ErrTagNotFound
    }
    return nil
}
```

Cascading FK on `link_tags` handles removing associations automatically (S6.12, S19).

### 3.5 `TagCount`

```go
func (s *SQLiteStore) TagCount(ctx context.Context) (int, error) {
    var count int
    err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags`).Scan(&count)
    return count, err
}
```

### 3.6 `SetLinkTags`

This is used during link creation and link update. It replaces all tags for a link (S6.6 — full replacement, not merge).

```go
func (s *SQLiteStore) SetLinkTags(ctx context.Context, linkID int64, tagNames []string) error {
    tx, err := s.writeDB.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // 1. Delete existing associations
    _, err = tx.ExecContext(ctx, `DELETE FROM link_tags WHERE link_id = ?`, linkID)
    if err != nil {
        return err
    }

    if len(tagNames) == 0 {
        return tx.Commit()
    }

    // 2. Find which tags don't exist yet
    existingTags := make(map[string]bool)
    placeholders := make([]string, len(tagNames))
    queryArgs := make([]interface{}, len(tagNames))
    for i, name := range tagNames {
        placeholders[i] = "?"
        queryArgs[i] = name
    }
    rows, err := tx.QueryContext(ctx,
        fmt.Sprintf(`SELECT name FROM tags WHERE name IN (%s)`,
            strings.Join(placeholders, ",")),
        queryArgs...)
    if err != nil {
        return err
    }
    for rows.Next() {
        var name string
        if err := rows.Scan(&name); err != nil {
            rows.Close()
            return err
        }
        existingTags[name] = true
    }
    rows.Close()

    // Count new tags that will be created
    newTagCount := 0
    for _, name := range tagNames {
        if !existingTags[name] {
            newTagCount++
        }
    }

    // 2b. Enforce 100-tag limit: check existing count + new tags
    if newTagCount > 0 {
        var existingCount int
        err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags`).Scan(&existingCount)
        if err != nil {
            return err
        }
        if existingCount+newTagCount > 100 {
            return fmt.Errorf("tag limit reached (max 100)")
            // Handler should catch this and return 400 {"error": "tag limit reached (max 100)"}
        }
    }

    // 3. Ensure all tags exist (INSERT OR IGNORE for idempotency)
    for _, name := range tagNames {
        _, err = tx.ExecContext(ctx,
            `INSERT OR IGNORE INTO tags (name) VALUES (?)`, name)
        if err != nil {
            return err
        }
    }

    // 3. Insert link_tags associations
    // Reuse placeholders and args slices from step 2
    placeholders = make([]string, len(tagNames))
    args = make([]interface{}, 0, len(tagNames)+1)
    args = append(args, linkID)
    for i, name := range tagNames {
        placeholders[i] = "?"
        args = append(args, name)
    }

    query := fmt.Sprintf(
        `INSERT INTO link_tags (link_id, tag_id)
         SELECT ?, id FROM tags WHERE name IN (%s)`,
        strings.Join(placeholders, ","))

    _, err = tx.ExecContext(ctx, query, args...)
    if err != nil {
        return err
    }

    return tx.Commit()
}
```

**Important**: Before calling `SetLinkTags` during link creation, the handler must check `TagCount` and verify that creating new tags won't exceed the 100 limit. Calculate: current tag count + number of tag names that don't already exist. If it would exceed 100, return 400.

### 3.7 `GetLinkTags`

```go
func (s *SQLiteStore) GetLinkTags(ctx context.Context, linkID int64) ([]string, error) {
    rows, err := s.readDB.QueryContext(ctx,
        `SELECT t.name FROM tags t
         INNER JOIN link_tags lt ON lt.tag_id = t.id
         WHERE lt.link_id = ?
         ORDER BY t.name ASC`, linkID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var names []string
    for rows.Next() {
        var name string
        if err := rows.Scan(&name); err != nil {
            return nil, err
        }
        names = append(names, name)
    }
    return names, rows.Err()
}
```

### 3.8 `GetLinksTagsBatch`

Used by `ListLinks` to efficiently load tags for a page of results in a single query, avoiding N+1.

```go
func (s *SQLiteStore) GetLinksTagsBatch(ctx context.Context, linkIDs []int64) (map[int64][]string, error) {
    if len(linkIDs) == 0 {
        return map[int64][]string{}, nil
    }

    placeholders := make([]string, len(linkIDs))
    args := make([]interface{}, len(linkIDs))
    for i, id := range linkIDs {
        placeholders[i] = "?"
        args[i] = id
    }

    query := fmt.Sprintf(
        `SELECT lt.link_id, t.name FROM link_tags lt
         INNER JOIN tags t ON t.id = lt.tag_id
         WHERE lt.link_id IN (%s)
         ORDER BY t.name ASC`,
        strings.Join(placeholders, ","))

    rows, err := s.readDB.QueryContext(ctx, query, args...)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    result := make(map[int64][]string)
    for rows.Next() {
        var linkID int64
        var name string
        if err := rows.Scan(&linkID, &name); err != nil {
            return nil, err
        }
        result[linkID] = append(result[linkID], name)
    }
    return result, rows.Err()
}
```

### 3.9 Update `ListLinks` — Add Tag Filter

Modify the existing `ListLinks` query builder. When the `tag` query parameter is present, add a subquery filter:

```sql
-- Add to WHERE clause when tag filter is set:
AND l.id IN (
    SELECT lt.link_id FROM link_tags lt
    INNER JOIN tags t ON t.id = lt.tag_id
    WHERE t.name = ?
)
```

Add `Tag string` field to the `ListLinksParams` struct used by the store.

---

## Step 4: Tag Handler — `backend/internal/handler/tags.go`

### Tag Name Validation

Tag names must match `^[a-zA-Z0-9_-]{1,50}$` (S6.2, S6.11):

```go
var tagNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,50}$`)

func validateTagName(name string) error {
    if !tagNameRegex.MatchString(name) {
        return fmt.Errorf("invalid tag name: must be 1-50 alphanumeric, dash, or underscore")
    }
    return nil
}
```

### Handler Struct

```go
type TagHandler struct {
    store store.Store
}

func NewTagHandler(s store.Store) *TagHandler {
    return &TagHandler{store: s}
}
```

### `GET /api/v1/tags` (S6.10)

```go
func (h *TagHandler) List(c echo.Context) error {
    tags, err := h.store.ListTags(c.Request().Context())
    if err != nil {
        return internalError(c, err)
    }
    if tags == nil {
        tags = []model.TagWithCount{}
    }
    return c.JSON(http.StatusOK, map[string]interface{}{
        "tags": tags,
    })
}
```

Response shape per S6.10:
```json
{
  "tags": [
    {"id": 1, "name": "marketing", "created_at": "...", "link_count": 15}
  ]
}
```

### `POST /api/v1/tags` (S6.11)

Request body:
```go
type createTagRequest struct {
    Name string `json:"name"`
}
```

```go
func (h *TagHandler) Create(c echo.Context) error {
    var req createTagRequest
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, errorResponse("invalid request body"))
    }

    req.Name = strings.TrimSpace(req.Name)
    if err := validateTagName(req.Name); err != nil {
        return c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
    }

    // Enforce 100 tag limit (S6.11)
    count, err := h.store.TagCount(c.Request().Context())
    if err != nil {
        return internalError(c, err)
    }
    if count >= 100 {
        return c.JSON(http.StatusBadRequest, errorResponse("tag limit reached (max 100)"))
    }

    tag, err := h.store.CreateTag(c.Request().Context(), req.Name)
    if err != nil {
        if errors.Is(err, store.ErrTagExists) {
            return c.JSON(http.StatusConflict, errorResponse("tag already exists"))
        }
        return internalError(c, err)
    }

    return c.JSON(http.StatusCreated, tag)
}
```

Response 201 per S6.11:
```json
{"id": 1, "name": "marketing", "created_at": "2026-03-26T10:00:00Z"}
```

### `DELETE /api/v1/tags/:id` (S6.12)

```go
func (h *TagHandler) Delete(c echo.Context) error {
    id, err := strconv.ParseInt(c.Param("id"), 10, 64)
    if err != nil {
        return c.JSON(http.StatusBadRequest, errorResponse("invalid tag ID"))
    }

    err = h.store.DeleteTag(c.Request().Context(), id)
    if err != nil {
        if errors.Is(err, store.ErrTagNotFound) {
            return c.JSON(http.StatusNotFound, errorResponse("not found"))
        }
        return internalError(c, err)
    }

    return c.NoContent(http.StatusNoContent)
}
```

---

## Step 5: Update Link Handlers

### 5.1 Create Link (`POST /api/v1/links`) — Add Tag Support

Update the create link request struct:

```go
type createLinkRequest struct {
    URL        string   `json:"url"`
    CustomCode string   `json:"custom_code"`
    ExpiresIn  *int     `json:"expires_in"`
    Tags       []string `json:"tags"`
}
```

After URL validation and code generation, before returning:

1. Validate each tag name against `tagNameRegex`. If any fail, return 400 with `"invalid tag name: must be 1-50 alphanumeric, dash, or underscore"` (S6.2). The entire request fails — do not create the link.
2. If tags are present, check whether creating new tags would exceed the 100 limit.
3. Create the link in the store.
4. Call `store.SetLinkTags(ctx, link.ID, tags)` to associate tags.
5. Include `"tags"` in the response JSON.

```go
// Validate and trim tag names before creating the link
var trimmedTags []string
if len(req.Tags) > 0 {
    trimmedTags = make([]string, 0, len(req.Tags))
    for _, name := range req.Tags {
        trimmed := strings.TrimSpace(name)
        if err := validateTagName(trimmed); err != nil {
            return c.JSON(http.StatusBadRequest, errorResponse(
                "invalid tag name: must be 1-50 alphanumeric, dash, or underscore"))
        }
        trimmedTags = append(trimmedTags, trimmed)
    }
}

// ... create link ...

// Set tags after link creation — pass trimmedTags (not req.Tags)
if len(trimmedTags) > 0 {
    if err := h.store.SetLinkTags(ctx, link.ID, trimmedTags); err != nil {
        return internalError(c, err)
    }
}
```

### 5.2 Update Link (`PATCH /api/v1/links/:id`) — Tag Replacement

Update the patch request struct:

```go
type updateLinkRequest struct {
    IsActive  *bool      `json:"is_active"`
    ExpiresAt *time.Time `json:"expires_at"`
    Tags      *[]string  `json:"tags"` // pointer to distinguish absent vs empty
}
```

When `Tags` is non-nil, validate all names, then call `SetLinkTags`. An empty slice (`"tags": []`) clears all tags. A missing field (`Tags == nil`) leaves tags unchanged (S6.6).

```go
if req.Tags != nil {
    trimmedTags := make([]string, 0, len(*req.Tags))
    for _, name := range *req.Tags {
        trimmed := strings.TrimSpace(name)
        if err := validateTagName(trimmed); err != nil {
            return c.JSON(http.StatusBadRequest, errorResponse(
                "invalid tag name: must be 1-50 alphanumeric, dash, or underscore"))
        }
        trimmedTags = append(trimmedTags, trimmed)
    }
    // Pass trimmedTags (not *req.Tags) to store trimmed values
    if err := h.store.SetLinkTags(ctx, id, trimmedTags); err != nil {
        return internalError(c, err)
    }
}
```

### 5.3 List Links (`GET /api/v1/links`) — Tag Filter + Tags in Response

Add `tag` query parameter parsing:

```go
params.Tag = c.QueryParam("tag")
```

After fetching links, load tags in batch:

```go
links, total, err := h.store.ListLinks(ctx, params)
// ...

// Collect link IDs
ids := make([]int64, len(links))
for i, l := range links {
    ids[i] = l.ID
}

// Batch load tags
tagMap, err := h.store.GetLinksTagsBatch(ctx, ids)
if err != nil {
    return internalError(c, err)
}

// Build response with tags
type linkResponse struct {
    model.Link
    Tags []string `json:"tags"`
}
responses := make([]linkResponse, len(links))
for i, l := range links {
    tags := tagMap[l.ID]
    if tags == nil {
        tags = []string{}
    }
    responses[i] = linkResponse{Link: l, Tags: tags}
}
```

### 5.4 Get Single Link (`GET /api/v1/links/:id`) — Tags in Response

After fetching the link, also fetch tags:

```go
tags, err := h.store.GetLinkTags(ctx, link.ID)
if err != nil {
    return internalError(c, err)
}
if tags == nil {
    tags = []string{}
}
```

Include `"tags"` in the JSON response.

---

## Step 6: Route Registration

In `backend/cmd/shorty/main.go`, register tag routes under the authenticated API group:

```go
tagHandler := handler.NewTagHandler(store)
apiV1 := e.Group("/api/v1", authMiddleware)

apiV1.GET("/tags", tagHandler.List)
apiV1.POST("/tags", tagHandler.Create)
apiV1.DELETE("/tags/:id", tagHandler.Delete)
```

---

## Step 7: Error Handling

All error responses use the standard `{"error": "message"}` format (S13).

| Condition | Status | Error Message |
|-----------|--------|---------------|
| Invalid tag name format | 400 | `"invalid tag name: must be 1-50 alphanumeric, dash, or underscore"` |
| Tag limit reached | 400 | `"tag limit reached (max 100)"` |
| Tag name already exists | 409 | `"tag already exists"` |
| Tag not found | 404 | `"not found"` |
| Invalid tag ID param | 400 | `"invalid tag ID"` |

---

## Step 8: Testing

### Store Tests (`sqlite_test.go`)

1. **CreateTag**: create a tag, verify fields. Create duplicate, verify `ErrTagExists`.
2. **ListTags**: create 3 tags, assign 2 to a link, verify `link_count` values (2, 0, 0).
3. **DeleteTag**: create tag, assign to link, delete tag, verify link still exists, verify `GetLinkTags` returns empty.
4. **TagCount**: create N tags, verify count.
5. **SetLinkTags**: create link, set tags `["a","b"]`, verify. Set tags `["b","c"]`, verify full replacement (a removed, c added). Set tags `[]`, verify empty.
6. **GetLinksTagsBatch**: create 3 links with different tags, batch load, verify map.
7. **ListLinks with tag filter**: create links with tags, filter by specific tag, verify only matching links returned.
8. **100 tag limit**: create 100 tags, attempt 101st via handler, verify 400.

### Handler Tests (`tags_test.go`)

Use mock store (interface-based) with `echo.NewContext`:

1. **List tags** — mock returns tags, verify 200 + response shape.
2. **Create tag** — valid name, verify 201. Invalid name (e.g. `"has spaces"`), verify 400. Duplicate, verify 409. At limit, verify 400.
3. **Delete tag** — exists, verify 204. Not found, verify 404.

### Integration Tests

1. Create a link with tags via `POST /api/v1/links`, verify tags in 201 response.
2. List links, verify tags present on each link.
3. Filter links by tag, verify filtering.
4. Update link tags via `PATCH /api/v1/links/:id`, verify replacement.
5. Delete tag, verify link's tags are updated.

### Verification Commands

```bash
cd backend && go test ./internal/store/... -run TestTag -race -v
cd backend && go test ./internal/handler/... -run TestTag -race -v
cd backend && go test ./... -race -count=1
```
