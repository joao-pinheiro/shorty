package handler

import (
	"context"
	"encoding/json"
	"fmt"
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

// --- Rate Limiter ---

func TestRateLimiterStore_GetLimiter(t *testing.T) {
	s := NewRateLimiterStore(RateLimitDefault)

	l1 := s.GetLimiter("1.2.3.4")
	if l1 == nil {
		t.Fatal("expected non-nil limiter")
	}

	l2 := s.GetLimiter("1.2.3.4")
	if l1 != l2 {
		t.Error("expected same limiter for same IP")
	}

	l3 := s.GetLimiter("5.6.7.8")
	if l1 == l3 {
		t.Error("expected different limiter for different IP")
	}
}

func TestRateLimiterStore_Cleanup(t *testing.T) {
	s := NewRateLimiterStore(RateLimitDefault)

	l1 := s.GetLimiter("1.2.3.4")

	// Force lastSeen to the past
	s.mu.Lock()
	s.limiters["1.2.3.4"].lastSeen = time.Now().Add(-20 * time.Minute)
	s.mu.Unlock()

	s.Cleanup()

	l2 := s.GetLimiter("1.2.3.4")
	if l1 == l2 {
		t.Error("expected new limiter after cleanup")
	}
}

func TestRateLimiterStore_StartCleanup(t *testing.T) {
	s := NewRateLimiterStore(RateLimitDefault)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.StartCleanup(ctx)
		close(done)
	}()
	cancel()
	<-done
}

func TestRateLimitMiddleware_AllowsRequests(t *testing.T) {
	s := NewRateLimiterStore(RateLimitConfig{Rate: 100, Burst: 100})
	mw := RateLimitMiddleware(s, true)

	e := echo.New()
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := mw(handler)(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("expected X-RateLimit-Limit header")
	}
}

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	s := NewRateLimiterStore(RateLimitConfig{Rate: 0.001, Burst: 1})
	mw := RateLimitMiddleware(s, false)

	e := echo.New()
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := mw(handler)(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_Blocks(t *testing.T) {
	s := NewRateLimiterStore(RateLimitConfig{Rate: 0.001, Burst: 1})
	mw := RateLimitMiddleware(s, true)

	e := echo.New()
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	// First request should pass
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	mw(handler)(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", rec.Code)
	}

	// Second request should be blocked
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	mw(handler)(c)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}

	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}
	if rec.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("expected X-RateLimit-Limit header")
	}
	if rec.Header().Get("X-RateLimit-Remaining") == "" {
		t.Error("expected X-RateLimit-Remaining header")
	}
	if rec.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("expected X-RateLimit-Reset header")
	}

	var body map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] != "rate limit exceeded" {
		t.Errorf("expected 'rate limit exceeded', got %v", body["error"])
	}
	if body["retry_after"] == nil {
		t.Error("expected retry_after in body")
	}
}

// --- Security Headers ---

func TestSecurityHeadersMiddleware(t *testing.T) {
	mw := SecurityHeadersMiddleware()

	e := echo.New()
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	mw(handler)(c)
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("expected X-Content-Type-Options: nosniff, got %q", rec.Header().Get("X-Content-Type-Options"))
	}
}

// --- Custom Error Handler ---

func TestCustomHTTPErrorHandler_EchoError(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	CustomHTTPErrorHandler(echo.NewHTTPError(http.StatusNotFound, "not found"), c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
	body := parseJSON(t, rec)
	if body["error"] != "not found" {
		t.Errorf("expected 'not found', got %v", body["error"])
	}
}

func TestCustomHTTPErrorHandler_PlainError(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	CustomHTTPErrorHandler(fmt.Errorf("something went wrong"), c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	body := parseJSON(t, rec)
	if body["error"] != "internal server error" {
		t.Errorf("expected 'internal server error', got %v", body["error"])
	}
}

func TestCustomHTTPErrorHandler_BodyLimit(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	CustomHTTPErrorHandler(echo.NewHTTPError(http.StatusRequestEntityTooLarge), c)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rec.Code)
	}
	body := parseJSON(t, rec)
	if body["error"] != "request body too large" {
		t.Errorf("expected 'request body too large', got %v", body["error"])
	}
}

func TestCustomHTTPErrorHandler_NonStringMessage(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	CustomHTTPErrorHandler(echo.NewHTTPError(http.StatusBadRequest, 12345), c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	body := parseJSON(t, rec)
	if body["error"] != "Bad Request" {
		t.Errorf("expected 'Bad Request', got %v", body["error"])
	}
}

// --- Request Logger ---

func TestRequestLoggerMiddleware(t *testing.T) {
	mw := RequestLoggerMiddleware()

	e := echo.New()
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := mw(handler)(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

// --- Link Handler additional edge cases ---

func TestLinkHandler_Create_EmptyURL(t *testing.T) {
	h := NewLinkHandler(&MockStore{}, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(`{"url": ""}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkHandler_Create_CustomCodeTooShort(t *testing.T) {
	h := NewLinkHandler(&MockStore{}, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(`{"url": "https://example.com", "custom_code": "a"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkHandler_Create_StoreCodeConflict(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		CreateLinkFn: func(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
			return nil, store.ErrCodeExists
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(`{"url": "https://example.com", "custom_code": "mycode"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkHandler_Update_NoFields(t *testing.T) {
	ms := &MockStore{
		UpdateLinkFn: func(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc", OriginalURL: "https://example.com", IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
		GetLinkTagsFn: func(ctx context.Context, linkID int64) ([]string, error) {
			return []string{}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/links/1", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Update(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkHandler_Update_InvalidID(t *testing.T) {
	h := NewLinkHandler(&MockStore{}, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/links/abc", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("abc")

	h.Update(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestLinkHandler_Update_WithTags(t *testing.T) {
	ms := &MockStore{
		UpdateLinkFn: func(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc", OriginalURL: "https://example.com", IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
		SetLinkTagsFn: func(ctx context.Context, linkID int64, tagNames []string) error {
			return nil
		},
		GetLinkTagsFn: func(ctx context.Context, linkID int64) ([]string, error) {
			return []string{"new-tag"}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/links/1", strings.NewReader(`{"tags": ["new-tag"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Update(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkHandler_Update_InvalidTagName(t *testing.T) {
	ms := &MockStore{} // won't be called

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/links/1", strings.NewReader(`{"tags": ["bad tag!"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Update(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkHandler_List_DefaultParams(t *testing.T) {
	var capturedParams store.ListParams
	ms := &MockStore{
		ListLinksFn: func(ctx context.Context, params store.ListParams) (*store.ListResult, error) {
			capturedParams = params
			return &store.ListResult{Links: []model.Link{}, Total: 0}, nil
		},
		GetLinksTagsBatchFn: func(ctx context.Context, linkIDs []int64) (map[int64][]string, error) {
			return map[int64][]string{}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.List(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedParams.Page != 1 {
		t.Errorf("expected page 1, got %d", capturedParams.Page)
	}
	if capturedParams.PerPage != 20 {
		t.Errorf("expected per_page 20, got %d", capturedParams.PerPage)
	}
}

func TestLinkHandler_List_PerPageCapped(t *testing.T) {
	var capturedParams store.ListParams
	ms := &MockStore{
		ListLinksFn: func(ctx context.Context, params store.ListParams) (*store.ListResult, error) {
			capturedParams = params
			return &store.ListResult{Links: []model.Link{}, Total: 0}, nil
		},
		GetLinksTagsBatchFn: func(ctx context.Context, linkIDs []int64) (map[int64][]string, error) {
			return map[int64][]string{}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links?per_page=200", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.List(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedParams.PerPage != 100 {
		t.Errorf("expected per_page capped to 100, got %d", capturedParams.PerPage)
	}
}

func TestLinkHandler_List_ActiveFilter(t *testing.T) {
	var capturedParams store.ListParams
	ms := &MockStore{
		ListLinksFn: func(ctx context.Context, params store.ListParams) (*store.ListResult, error) {
			capturedParams = params
			return &store.ListResult{Links: []model.Link{}, Total: 0}, nil
		},
		GetLinksTagsBatchFn: func(ctx context.Context, linkIDs []int64) (map[int64][]string, error) {
			return map[int64][]string{}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links?active=true", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.List(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedParams.Active == nil || *capturedParams.Active != true {
		t.Error("expected active filter to be true")
	}
}

// --- QR Code Handler ---

func TestLinkHandler_QRCode_Success(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc", OriginalURL: "https://example.com", IsActive: true}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/qr", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	err := h.QRCode(c)
	if err != nil {
		t.Fatalf("qr: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("expected Content-Type image/png, got %q", ct)
	}
	body := rec.Body.Bytes()
	if len(body) < 4 || body[0] != 0x89 || body[1] != 0x50 {
		t.Error("response is not a valid PNG")
	}
}

func TestLinkHandler_QRCode_NotFound(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return nil, store.ErrNotFound
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/999/qr", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	h.QRCode(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestLinkHandler_QRCode_InvalidID(t *testing.T) {
	h := NewLinkHandler(&MockStore{}, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/abc/qr", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("abc")

	h.QRCode(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestLinkHandler_QRCode_CustomSize(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc", OriginalURL: "https://example.com", IsActive: true}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/qr?size=512", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.QRCode(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestLinkHandler_QRCode_SizeClamped(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc", OriginalURL: "https://example.com", IsActive: true}, nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	// Size too small
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/qr?size=10", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	h.QRCode(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for small size, got %d", rec.Code)
	}

	// Size too large
	req = httptest.NewRequest(http.MethodGet, "/api/v1/links/1/qr?size=9999", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")
	h.QRCode(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for large size, got %d", rec.Code)
	}
}

func TestLinkHandler_Create_WithTags(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		CreateLinkFn: func(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
			return &model.Link{ID: 1, Code: code, OriginalURL: originalURL, CreatedAt: time.Now(), IsActive: true, UpdatedAt: time.Now()}, nil
		},
		SetLinkTagsFn: func(ctx context.Context, linkID int64, tagNames []string) error {
			return nil
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(`{"url": "https://example.com", "tags": ["test-tag"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkHandler_Create_InvalidTagName(t *testing.T) {
	h := NewLinkHandler(&MockStore{}, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(`{"url": "https://example.com", "tags": ["bad tag!"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestLinkHandler_Get_InvalidID(t *testing.T) {
	h := NewLinkHandler(&MockStore{}, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/abc", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("abc")

	h.Get(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestLinkHandler_Delete_InvalidID(t *testing.T) {
	h := NewLinkHandler(&MockStore{}, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/links/abc", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("abc")

	h.Delete(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// --- Analytics Handler Unit Tests ---

func TestAnalyticsHandler_Get_Success_7d(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc"}, nil
		},
		GetTotalClickCountFn: func(ctx context.Context, linkID int64) (int, error) {
			return 10, nil
		},
		GetPeriodClickCountFn: func(ctx context.Context, linkID int64, since time.Time) (int, error) {
			return 5, nil
		},
		GetClicksByDayFn: func(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error) {
			return []model.DayCount{{Date: "2026-03-25", Count: 5}}, nil
		},
	}

	h := NewAnalyticsHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/analytics?period=7d", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	body := parseJSON(t, rec)
	if body["clicks_by_day"] == nil {
		t.Error("expected clicks_by_day")
	}
}

func TestAnalyticsHandler_Get_Success_24h(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc"}, nil
		},
		GetTotalClickCountFn: func(ctx context.Context, linkID int64) (int, error) {
			return 10, nil
		},
		GetPeriodClickCountFn: func(ctx context.Context, linkID int64, since time.Time) (int, error) {
			return 3, nil
		},
		GetClicksByHourFn: func(ctx context.Context, linkID int64, since time.Time) ([]model.HourCount, error) {
			return nil, nil
		},
	}

	h := NewAnalyticsHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/analytics?period=24h", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := parseJSON(t, rec)
	if body["clicks_by_hour"] == nil {
		t.Error("expected clicks_by_hour")
	}
}

func TestAnalyticsHandler_Get_Success_30d(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc"}, nil
		},
		GetTotalClickCountFn: func(ctx context.Context, linkID int64) (int, error) {
			return 10, nil
		},
		GetPeriodClickCountFn: func(ctx context.Context, linkID int64, since time.Time) (int, error) {
			return 7, nil
		},
		GetClicksByDayFn: func(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error) {
			return nil, nil
		},
	}

	h := NewAnalyticsHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/analytics?period=30d", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAnalyticsHandler_Get_Success_All(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc"}, nil
		},
		GetTotalClickCountFn: func(ctx context.Context, linkID int64) (int, error) {
			return 10, nil
		},
		GetPeriodClickCountFn: func(ctx context.Context, linkID int64, since time.Time) (int, error) {
			return 10, nil
		},
		GetClicksByDayFn: func(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error) {
			return nil, nil
		},
	}

	h := NewAnalyticsHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/analytics?period=all", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAnalyticsHandler_Get_DefaultPeriod(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc"}, nil
		},
		GetTotalClickCountFn: func(ctx context.Context, linkID int64) (int, error) {
			return 0, nil
		},
		GetPeriodClickCountFn: func(ctx context.Context, linkID int64, since time.Time) (int, error) {
			return 0, nil
		},
		GetClicksByDayFn: func(ctx context.Context, linkID int64, since time.Time) ([]model.DayCount, error) {
			return nil, nil
		},
	}

	h := NewAnalyticsHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/analytics", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAnalyticsHandler_Get_InvalidPeriod(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc"}, nil
		},
	}

	h := NewAnalyticsHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/analytics?period=bad", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAnalyticsHandler_Get_InvalidID(t *testing.T) {
	h := NewAnalyticsHandler(&MockStore{})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/abc/analytics", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("abc")

	h.Get(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAnalyticsHandler_Get_NotFound(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return nil, store.ErrNotFound
		},
	}

	h := NewAnalyticsHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/999/analytics", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("999")

	h.Get(c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAnalyticsHandler_Get_StoreError(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewAnalyticsHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/analytics", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// --- Bulk Handler Unit Tests ---

func TestBulkHandler_Create_Success(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		CreateLinkFn: func(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
			return &model.Link{ID: 1, Code: code, OriginalURL: originalURL, IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
		SetLinkTagsFn: func(ctx context.Context, linkID int64, tagNames []string) error {
			return nil
		},
	}

	h := NewBulkHandler(ms, testConfig())

	e := echo.New()
	body := `{"urls": [{"url": "https://example.com/1"}, {"url": "https://example.com/2", "tags": ["t1"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	result := parseJSON(t, rec)
	if int(result["succeeded"].(float64)) != 2 {
		t.Errorf("expected 2 succeeded, got %v", result["succeeded"])
	}
}

func TestBulkHandler_Create_Empty(t *testing.T) {
	h := NewBulkHandler(&MockStore{}, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links/bulk", strings.NewReader(`{"urls": []}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestBulkHandler_Create_ExceedsMax(t *testing.T) {
	h := NewBulkHandler(&MockStore{}, testConfig())

	urls := make([]string, 51)
	for i := range urls {
		urls[i] = fmt.Sprintf(`{"url": "https://example.com/%d"}`, i)
	}
	body := `{"urls": [` + strings.Join(urls, ",") + `]}`

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestBulkHandler_Create_InvalidURL(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
	}

	h := NewBulkHandler(ms, testConfig())

	e := echo.New()
	body := `{"urls": [{"url": "not-a-url"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	result := parseJSON(t, rec)
	if int(result["failed"].(float64)) != 1 {
		t.Errorf("expected 1 failed, got %v", result["failed"])
	}
}

func TestBulkHandler_Create_CustomCodeConflict(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return true, nil
		},
	}

	h := NewBulkHandler(ms, testConfig())

	e := echo.New()
	body := `{"urls": [{"url": "https://example.com", "custom_code": "taken"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	result := parseJSON(t, rec)
	if int(result["failed"].(float64)) != 1 {
		t.Errorf("expected 1 failed, got %v", result["failed"])
	}
}

func TestBulkHandler_Create_InvalidExpiresIn(t *testing.T) {
	h := NewBulkHandler(&MockStore{}, testConfig())

	e := echo.New()
	body := `{"urls": [{"url": "https://example.com", "expires_in": -1}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	result := parseJSON(t, rec)
	if int(result["failed"].(float64)) != 1 {
		t.Errorf("expected 1 failed, got %v", result["failed"])
	}
}

func TestBulkHandler_Create_InvalidCustomCode(t *testing.T) {
	h := NewBulkHandler(&MockStore{}, testConfig())

	e := echo.New()
	body := `{"urls": [{"url": "https://example.com", "custom_code": "a"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	result := parseJSON(t, rec)
	if int(result["failed"].(float64)) != 1 {
		t.Errorf("expected 1 failed, got %v", result["failed"])
	}
}

func TestBulkHandler_Create_InvalidTag(t *testing.T) {
	h := NewBulkHandler(&MockStore{}, testConfig())

	e := echo.New()
	body := `{"urls": [{"url": "https://example.com", "tags": ["bad tag!"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	result := parseJSON(t, rec)
	if int(result["failed"].(float64)) != 1 {
		t.Errorf("expected 1 failed, got %v", result["failed"])
	}
}

func TestBulkHandler_Create_WithExpiresIn(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		CreateLinkFn: func(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
			return &model.Link{ID: 1, Code: code, OriginalURL: originalURL, IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
		SetLinkTagsFn: func(ctx context.Context, linkID int64, tagNames []string) error {
			return nil
		},
	}

	h := NewBulkHandler(ms, testConfig())

	e := echo.New()
	body := `{"urls": [{"url": "https://example.com", "expires_in": 3600}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	result := parseJSON(t, rec)
	if int(result["succeeded"].(float64)) != 1 {
		t.Errorf("expected 1 succeeded, got %v", result["succeeded"])
	}
}

// --- Tag Handler additional edge cases ---

func TestTagHandler_List_StoreError(t *testing.T) {
	ms := &MockStore{
		ListTagsFn: func(ctx context.Context) ([]model.TagWithCount, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewTagHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tags", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.List(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestTagHandler_Delete_InvalidID(t *testing.T) {
	h := NewTagHandler(&MockStore{})

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tags/abc", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("abc")

	h.Delete(c)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestTagHandler_Delete_StoreError(t *testing.T) {
	ms := &MockStore{
		DeleteTagFn: func(ctx context.Context, id int64) error {
			return fmt.Errorf("db error")
		},
	}

	h := NewTagHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tags/1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Delete(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestTagHandler_Create_StoreError(t *testing.T) {
	ms := &MockStore{
		TagCountFn: func(ctx context.Context) (int, error) {
			return 0, nil
		},
		CreateTagFn: func(ctx context.Context, name string) (*model.Tag, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewTagHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tags", strings.NewReader(`{"name": "test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestTagHandler_Create_TagCountError(t *testing.T) {
	ms := &MockStore{
		TagCountFn: func(ctx context.Context) (int, error) {
			return 0, fmt.Errorf("db error")
		},
	}

	h := NewTagHandler(ms)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tags", strings.NewReader(`{"name": "test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// --- Link Handler store error paths ---

func TestLinkHandler_Create_StoreError(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		CreateLinkFn: func(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(`{"url": "https://example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_Create_CodeExistsError(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(`{"url": "https://example.com", "custom_code": "mycode"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_List_StoreError(t *testing.T) {
	ms := &MockStore{
		ListLinksFn: func(ctx context.Context, params store.ListParams) (*store.ListResult, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.List(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_List_TagsBatchError(t *testing.T) {
	ms := &MockStore{
		ListLinksFn: func(ctx context.Context, params store.ListParams) (*store.ListResult, error) {
			return &store.ListResult{Links: []model.Link{{ID: 1, Code: "abc"}}, Total: 1}, nil
		},
		GetLinksTagsBatchFn: func(ctx context.Context, linkIDs []int64) (map[int64][]string, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.List(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_Get_StoreError(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_Get_TagsError(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc"}, nil
		},
		GetLinkTagsFn: func(ctx context.Context, linkID int64) ([]string, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Get(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_Update_StoreError(t *testing.T) {
	ms := &MockStore{
		UpdateLinkFn: func(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/links/1", strings.NewReader(`{"is_active": true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Update(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_Update_SetTagsError(t *testing.T) {
	ms := &MockStore{
		UpdateLinkFn: func(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc"}, nil
		},
		SetLinkTagsFn: func(ctx context.Context, linkID int64, tagNames []string) error {
			return fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/links/1", strings.NewReader(`{"tags": ["t1"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Update(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_Update_GetTagsError(t *testing.T) {
	ms := &MockStore{
		UpdateLinkFn: func(ctx context.Context, id int64, isActive *bool, expiresAt *string) (*model.Link, error) {
			return &model.Link{ID: 1, Code: "abc"}, nil
		},
		GetLinkTagsFn: func(ctx context.Context, linkID int64) ([]string, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/links/1", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Update(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_Delete_StoreError(t *testing.T) {
	ms := &MockStore{
		DeleteLinkFn: func(ctx context.Context, id int64) error {
			return fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/links/1", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.Delete(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_QRCode_StoreError(t *testing.T) {
	ms := &MockStore{
		GetLinkByIDFn: func(ctx context.Context, id int64) (*model.Link, error) {
			return nil, fmt.Errorf("db error")
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/1/qr", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("1")

	h.QRCode(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

func TestLinkHandler_Create_SetTagsError(t *testing.T) {
	ms := &MockStore{
		CodeExistsFn: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		CreateLinkFn: func(ctx context.Context, code, originalURL string, expiresAt *string) (*model.Link, error) {
			return &model.Link{ID: 1, Code: code, OriginalURL: originalURL, IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
		SetLinkTagsFn: func(ctx context.Context, linkID int64, tagNames []string) error {
			return fmt.Errorf("db error")
		},
		DeleteLinkFn: func(ctx context.Context, id int64) error {
			return nil // cleanup orphaned link
		},
	}

	h := NewLinkHandler(ms, testConfig())

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(`{"url": "https://example.com", "tags": ["t1"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h.Create(c)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}
