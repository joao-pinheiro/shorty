package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"shorty/internal/config"
	"shorty/internal/model"
	"shorty/internal/store"
)

// MockStore implements store.Store with function fields.
type MockStore struct {
	CreateLinkFn            func(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error)
	GetLinkByCodeFn         func(ctx context.Context, code string) (*model.Link, error)
	GetLinkByIDFn           func(ctx context.Context, id int64) (*model.Link, error)
	ListLinksFn             func(ctx context.Context, params store.ListParams) (*store.ListResult, error)
	UpdateLinkFn            func(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error)
	DeactivateExpiredLinkFn func(ctx context.Context, id int64) error
	DeleteLinkFn            func(ctx context.Context, id int64) error
	CodeExistsFn            func(ctx context.Context, code string) (bool, error)
	BatchInsertClicksFn     func(ctx context.Context, events []store.ClickEvent) error
	CreateTagFn             func(ctx context.Context, name string) (*model.Tag, error)
	ListTagsFn              func(ctx context.Context) ([]model.TagWithCount, error)
	TagCountFn              func(ctx context.Context) (int, error)
	GetTagByIDFn            func(ctx context.Context, id int64) (*model.Tag, error)
	DeleteTagFn             func(ctx context.Context, id int64) error
	SetLinkTagsFn           func(ctx context.Context, linkID int64, tagNames []string) error
	GetLinkTagsFn           func(ctx context.Context, linkID int64) ([]string, error)
	GetLinksTagsBatchFn     func(ctx context.Context, linkIDs []int64) (map[int64][]string, error)
	GetClicksByDayFn        func(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error)
	GetClicksByHourFn       func(ctx context.Context, linkID int64, since time.Time) ([]model.HourCount, error)
	GetPeriodClickCountFn   func(ctx context.Context, linkID int64, since time.Time) (int, error)
	GetTotalClickCountFn    func(ctx context.Context, linkID int64) (int, error)
	DeleteClicksOlderThanFn func(ctx context.Context, before time.Time) (int64, error)
	CloseFn                 func() error
}

func (m *MockStore) CreateLink(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
	return m.CreateLinkFn(ctx, code, originalURL, expiresAt)
}
func (m *MockStore) GetLinkByCode(ctx context.Context, code string) (*model.Link, error) {
	return m.GetLinkByCodeFn(ctx, code)
}
func (m *MockStore) GetLinkByID(ctx context.Context, id int64) (*model.Link, error) {
	return m.GetLinkByIDFn(ctx, id)
}
func (m *MockStore) ListLinks(ctx context.Context, params store.ListParams) (*store.ListResult, error) {
	return m.ListLinksFn(ctx, params)
}
func (m *MockStore) UpdateLink(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
	return m.UpdateLinkFn(ctx, id, isActive, expiresAt)
}
func (m *MockStore) DeactivateExpiredLink(ctx context.Context, id int64) error {
	return m.DeactivateExpiredLinkFn(ctx, id)
}
func (m *MockStore) DeleteLink(ctx context.Context, id int64) error {
	return m.DeleteLinkFn(ctx, id)
}
func (m *MockStore) CodeExists(ctx context.Context, code string) (bool, error) {
	return m.CodeExistsFn(ctx, code)
}
func (m *MockStore) BatchInsertClicks(ctx context.Context, events []store.ClickEvent) error {
	return m.BatchInsertClicksFn(ctx, events)
}
func (m *MockStore) CreateTag(ctx context.Context, name string) (*model.Tag, error) {
	return m.CreateTagFn(ctx, name)
}
func (m *MockStore) ListTags(ctx context.Context) ([]model.TagWithCount, error) {
	return m.ListTagsFn(ctx)
}
func (m *MockStore) TagCount(ctx context.Context) (int, error) {
	return m.TagCountFn(ctx)
}
func (m *MockStore) GetTagByID(ctx context.Context, id int64) (*model.Tag, error) {
	return m.GetTagByIDFn(ctx, id)
}
func (m *MockStore) DeleteTag(ctx context.Context, id int64) error {
	return m.DeleteTagFn(ctx, id)
}
func (m *MockStore) SetLinkTags(ctx context.Context, linkID int64, tagNames []string) error {
	return m.SetLinkTagsFn(ctx, linkID, tagNames)
}
func (m *MockStore) GetLinkTags(ctx context.Context, linkID int64) ([]string, error) {
	return m.GetLinkTagsFn(ctx, linkID)
}
func (m *MockStore) GetLinksTagsBatch(ctx context.Context, linkIDs []int64) (map[int64][]string, error) {
	return m.GetLinksTagsBatchFn(ctx, linkIDs)
}
func (m *MockStore) GetClicksByDay(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error) {
	return m.GetClicksByDayFn(ctx, linkID, since)
}
func (m *MockStore) GetClicksByHour(ctx context.Context, linkID int64, since time.Time) ([]model.HourCount, error) {
	return m.GetClicksByHourFn(ctx, linkID, since)
}
func (m *MockStore) GetPeriodClickCount(ctx context.Context, linkID int64, since time.Time) (int, error) {
	return m.GetPeriodClickCountFn(ctx, linkID, since)
}
func (m *MockStore) GetTotalClickCount(ctx context.Context, linkID int64) (int, error) {
	return m.GetTotalClickCountFn(ctx, linkID)
}
func (m *MockStore) DeleteClicksOlderThan(ctx context.Context, before time.Time) (int64, error) {
	return m.DeleteClicksOlderThanFn(ctx, before)
}
func (m *MockStore) Close() error {
	if m.CloseFn != nil {
		return m.CloseFn()
	}
	return nil
}

func testConfig() *config.Config {
	return &config.Config{
		BaseURL:           "http://localhost:8080",
		DefaultCodeLength: 6,
		MaxBulkURLs:       50,
	}
}

func parseJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return result
}

// --- Health Check ---

func TestHealthCheck(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := HealthCheck(c)
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %v", body["status"])
	}
	if body["version"] != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %v", body["version"])
	}
}

// --- Auth Middleware ---

func TestAuthMiddleware(t *testing.T) {
	apiKey := "test-secret-key"
	mw := AuthMiddleware(apiKey)
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	tests := []struct {
		name       string
		authHeader string
		wantCode   int
		wantError  string
	}{
		{
			name:       "missing header",
			authHeader: "",
			wantCode:   http.StatusUnauthorized,
			wantError:  "missing authorization header",
		},
		{
			name:       "wrong prefix",
			authHeader: "Basic abc123",
			wantCode:   http.StatusUnauthorized,
			wantError:  "invalid API key",
		},
		{
			name:       "wrong key",
			authHeader: "Bearer wrong-key",
			wantCode:   http.StatusUnauthorized,
			wantError:  "invalid API key",
		},
		{
			name:       "valid key",
			authHeader: "Bearer test-secret-key",
			wantCode:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := mw(handler)(c)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if rec.Code != tt.wantCode {
				t.Errorf("expected %d, got %d", tt.wantCode, rec.Code)
			}

			if tt.wantError != "" {
				body := parseJSON(t, rec)
				if body["error"] != tt.wantError {
					t.Errorf("expected error %q, got %q", tt.wantError, body["error"])
				}
			}
		})
	}
}

// --- Link Handler ---

func TestLinkCreate_Success(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		CreateLinkFn: func(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
			return &model.Link{
				ID:          1,
				Code:        code,
				OriginalURL: originalURL,
				CreatedAt:   time.Now(),
				IsActive:    true,
				UpdatedAt:   time.Now(),
			}, nil
		},
		SetLinkTagsFn: func(ctx context.Context, linkID int64, tagNames []string) error {
			return nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	body := `{"url": "https://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.Create(c)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}

	result := parseJSON(t, rec)
	if result["original_url"] != "https://example.com" {
		t.Errorf("expected URL in response, got %v", result["original_url"])
	}
	if result["short_url"] == nil || result["short_url"] == "" {
		t.Error("expected short_url in response")
	}
}

func TestLinkCreate_InvalidURL(t *testing.T) {
	h := NewLinkHandler(&MockStore{}, testConfig())

	e := echo.New()
	body := `{"url": "ftp://example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestLinkCreate_CustomCode(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		CreateLinkFn: func(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
			return &model.Link{
				ID:          1,
				Code:        code,
				OriginalURL: originalURL,
				CreatedAt:   time.Now(),
				IsActive:    true,
				UpdatedAt:   time.Now(),
			}, nil
		},
		SetLinkTagsFn: func(ctx context.Context, linkID int64, tagNames []string) error {
			return nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	body := `{"url": "https://example.com", "custom_code": "my-code"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	result := parseJSON(t, rec)
	if result["code"] != "my-code" {
		t.Errorf("expected code my-code, got %v", result["code"])
	}
}

func TestLinkCreate_ReservedCode(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	body := `{"url": "https://example.com", "custom_code": "api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}

	result := parseJSON(t, rec)
	if result["error"] != "code is reserved" {
		t.Errorf("expected 'code is reserved', got %v", result["error"])
	}
}

func TestLinkCreate_ExpiresInValidation(t *testing.T) {
	h := NewLinkHandler(&MockStore{}, testConfig())

	tests := []struct {
		name string
		body string
	}{
		{"zero", `{"url": "https://example.com", "expires_in": 0}`},
		{"negative", `{"url": "https://example.com", "expires_in": -1}`},
		{"too large", `{"url": "https://example.com", "expires_in": 99999999}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			h.Create(c)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLinkList(t *testing.T) {
	ms := &MockStore{
		ListLinksFn: func(ctx context.Context, params store.ListParams) (*store.ListResult, error) {
			return &store.ListResult{
				Links: []model.Link{
					{ID: 1, Code: "abc", OriginalURL: "https://example.com", IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()},
				},
				Total: 1,
			}, nil
		},
		GetLinksTagsBatchFn: func(ctx context.Context, linkIDs []int64) (map[int64][]string, error) {
			return map[int64][]string{1: {"tag1"}}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links?page=1&per_page=20", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := h.List(c)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	result := parseJSON(t, rec)
	if result["total"].(float64) != 1 {
		t.Errorf("expected total 1, got %v", result["total"])
	}
	links := result["links"].([]interface{})
	if len(links) != 1 {
		t.Errorf("expected 1 link, got %d", len(links))
	}
}

func TestLinkGet(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			if id == 1 {
				return &model.Link{ID: 1, Code: "abc", OriginalURL: "https://example.com", IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
			}
			return nil, store.ErrNotFound
		},
		GetLinkTagsFn: func(ctx context.Context, linkID int64) ([]string, error) {
			return []string{}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	// Found
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	// Not found
	req = httptest.NewRequest(http.MethodGet, "/api/v1/links/999", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	h.Get(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestLinkUpdate(t *testing.T) {
	ms := &MockStore{
		UpdateLinkFn: func(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
			if id == 1 {
				return &model.Link{ID: 1, Code: "abc", OriginalURL: "https://example.com", IsActive: false, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
			}
			return nil, store.ErrNotFound
		},
		GetLinkTagsFn: func(ctx context.Context, linkID int64) ([]string, error) {
			return []string{}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	// Update is_active
	e := echo.New()
	body := `{"is_active": false}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/links/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Update(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	// Invalid RFC3339
	body = `{"expires_at": "not-a-date"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/links/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Update(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid RFC3339, got %d", rec.Code)
	}

	// Not found
	body = `{"is_active": true}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/links/999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	h.Update(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestLinkDelete(t *testing.T) {
	ms := &MockStore{
		DeleteLinkFn: func(ctx context.Context, id int64) error {
			if id == 1 {
				return nil
			}
			return store.ErrNotFound
		},
	}

	h := NewLinkHandler(ms, testConfig())

	// Delete success
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/links/1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Delete(c)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}

	// Delete not found
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/links/999", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	h.Delete(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Tag Handler ---

func TestTagCreate_Success(t *testing.T) {
	ms := &MockStore{
		TagCountFn: func(ctx context.Context) (int, error) {
			return 0, nil
		},
		CreateTagFn: func(ctx context.Context, name string) (*model.Tag, error) {
			return &model.Tag{ID: 1, Name: name, CreatedAt: time.Now()}, nil
		},
	}

	h := NewTagHandler(ms)

	e := echo.New()
	body := `{"name": "marketing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tags", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	result := parseJSON(t, rec)
	if result["name"] != "marketing" {
		t.Errorf("expected name marketing, got %v", result["name"])
	}
}

func TestTagCreate_InvalidName(t *testing.T) {
	h := NewTagHandler(&MockStore{})

	tests := []struct {
		name string
		body string
	}{
		{"empty", `{"name": ""}`},
		{"spaces", `{"name": "has spaces"}`},
		{"too long", `{"name": "` + strings.Repeat("a", 51) + `"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tags", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			h.Create(c)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestTagCreate_Limit(t *testing.T) {
	ms := &MockStore{
		TagCountFn: func(ctx context.Context) (int, error) {
			return 100, nil
		},
	}

	h := NewTagHandler(ms)

	e := echo.New()
	body := `{"name": "overflow"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tags", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}

	result := parseJSON(t, rec)
	if result["error"] != "tag limit reached (max 100)" {
		t.Errorf("unexpected error: %v", result["error"])
	}
}

func TestTagCreate_Duplicate(t *testing.T) {
	ms := &MockStore{
		TagCountFn: func(ctx context.Context) (int, error) {
			return 1, nil
		},
		CreateTagFn: func(ctx context.Context, name string) (*model.Tag, error) {
			return nil, store.ErrTagExists
		},
	}

	h := NewTagHandler(ms)

	e := echo.New()
	body := `{"name": "existing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tags", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

func TestTagList(t *testing.T) {
	ms := &MockStore{
		ListTagsFn: func(ctx context.Context) ([]model.TagWithCount, error) {
			return []model.TagWithCount{
				{ID: 1, Name: "alpha", CreatedAt: time.Now(), LinkCount: 5},
			}, nil
		},
	}

	h := NewTagHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tags", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.List(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	result := parseJSON(t, rec)
	tags := result["tags"].([]interface{})
	if len(tags) != 1 {
		t.Errorf("expected 1 tag, got %d", len(tags))
	}
}

func TestTagDelete(t *testing.T) {
	ms := &MockStore{
		DeleteTagFn: func(ctx context.Context, id int64) error {
			if id == 1 {
				return nil
			}
			return store.ErrTagNotFound
		},
	}

	h := NewTagHandler(ms)

	// Success
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tags/1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Delete(c)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}

	// Not found
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/tags/999", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	h.Delete(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Code Exists Conflict ---

func TestLinkCreate_CodeConflict(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return true, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	body := `{"url": "https://example.com", "custom_code": "taken"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d; body: %s", rec.Code, rec.Body.String())
	}
}
