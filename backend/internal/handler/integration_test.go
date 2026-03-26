package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"shorty/internal/clickrecorder"
	"shorty/internal/config"
	"shorty/internal/migrations"
	"shorty/internal/store"
)

const testAPIKey = "test-key"

func setupTestServer(t *testing.T) (*echo.Echo, store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath, migrations.FS)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	cfg := &config.Config{
		BaseURL:           "http://localhost:8080",
		DefaultCodeLength: 6,
		MaxBulkURLs:       50,
		RateLimitEnabled:  false,
		APIKey:            testAPIKey,
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = CustomHTTPErrorHandler

	e.Use(SecurityHeadersMiddleware())
	e.Use(echomw.BodyLimit("1M"))

	recorder := clickrecorder.New(s, 1000, 60)

	linkHandler := NewLinkHandler(s, cfg)
	tagHandler := NewTagHandler(s)
	bulkHandler := NewBulkHandler(s, cfg)
	analyticsHandler := NewAnalyticsHandler(s)

	e.GET("/api/health", HealthCheck)

	apiV1 := e.Group("/api/v1", AuthMiddleware(cfg.APIKey))
	apiV1.POST("/links", linkHandler.Create)
	apiV1.POST("/links/bulk", bulkHandler.Create)
	apiV1.GET("/links", linkHandler.List)
	apiV1.GET("/links/:id", linkHandler.Get)
	apiV1.PATCH("/links/:id", linkHandler.Update)
	apiV1.DELETE("/links/:id", linkHandler.Delete)
	apiV1.GET("/links/:id/analytics", analyticsHandler.Get)
	apiV1.GET("/links/:id/qr", linkHandler.QRCode)
	apiV1.GET("/tags", tagHandler.List)
	apiV1.POST("/tags", tagHandler.Create)
	apiV1.DELETE("/tags/:id", tagHandler.Delete)

	// For CatchAll tests we need a minimal frontend handler.
	// We'll skip it for API tests and add it separately where needed.
	_ = recorder

	return e, s
}

func authReq(method, url string, body string) *http.Request {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, url, nil)
	}
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	return req
}

func doRequest(e *echo.Echo, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func parseBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return result
}

// createLink is a helper that creates a link and returns its ID.
func createLink(t *testing.T, e *echo.Echo, url string) (int64, string) {
	t.Helper()
	body := fmt.Sprintf(`{"url": %q}`, url)
	rec := doRequest(e, authReq(http.MethodPost, "/api/v1/links", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create link failed: %d %s", rec.Code, rec.Body.String())
	}
	result := parseBody(t, rec)
	id := int64(result["id"].(float64))
	code := result["code"].(string)
	return id, code
}

// --- Bulk Create ---

func TestIntegration_BulkCreate(t *testing.T) {
	e, _ := setupTestServer(t)

	body := `{"urls": [
		{"url": "https://example.com/1"},
		{"url": "not-a-url"},
		{"url": "https://example.com/2"}
	]}`
	rec := doRequest(e, authReq(http.MethodPost, "/api/v1/links/bulk", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseBody(t, rec)
	if int(result["total"].(float64)) != 3 {
		t.Errorf("expected total 3, got %v", result["total"])
	}
	if int(result["succeeded"].(float64)) != 2 {
		t.Errorf("expected succeeded 2, got %v", result["succeeded"])
	}
	if int(result["failed"].(float64)) != 1 {
		t.Errorf("expected failed 1, got %v", result["failed"])
	}

	results := result["results"].([]interface{})
	// Index 1 should have failed
	item1 := results[1].(map[string]interface{})
	if item1["ok"].(bool) {
		t.Error("expected index 1 to fail")
	}
	if item1["index"] == nil {
		t.Error("expected index field on failed item")
	}
}

func TestIntegration_BulkCreate_ExceedsMax(t *testing.T) {
	e, _ := setupTestServer(t)

	urls := make([]string, 51)
	for i := range urls {
		urls[i] = fmt.Sprintf(`{"url": "https://example.com/%d"}`, i)
	}
	body := `{"urls": [` + strings.Join(urls, ",") + `]}`

	rec := doRequest(e, authReq(http.MethodPost, "/api/v1/links/bulk", body))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestIntegration_BulkCreate_Empty(t *testing.T) {
	e, _ := setupTestServer(t)

	rec := doRequest(e, authReq(http.MethodPost, "/api/v1/links/bulk", `{"urls": []}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// --- Analytics ---

func TestIntegration_Analytics_7d(t *testing.T) {
	e, s := setupTestServer(t)

	id, _ := createLink(t, e, "https://example.com/analytics")

	// Insert clicks directly
	now := time.Now().UTC()
	events := []store.ClickEvent{
		{LinkID: id, ClickedAt: now.Add(-1 * time.Hour)},
		{LinkID: id, ClickedAt: now.Add(-2 * time.Hour)},
		{LinkID: id, ClickedAt: now.Add(-25 * time.Hour)},
	}
	if err := s.BatchInsertClicks(context.Background(), events); err != nil {
		t.Fatalf("insert clicks: %v", err)
	}

	rec := doRequest(e, authReq(http.MethodGet, fmt.Sprintf("/api/v1/links/%d/analytics?period=7d", id), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseBody(t, rec)
	if result["clicks_by_day"] == nil {
		t.Error("expected clicks_by_day in response")
	}
	if int(result["total_clicks"].(float64)) != 3 {
		t.Errorf("expected total_clicks 3, got %v", result["total_clicks"])
	}
}

func TestIntegration_Analytics_24h(t *testing.T) {
	e, s := setupTestServer(t)

	id, _ := createLink(t, e, "https://example.com/analytics24")

	now := time.Now().UTC()
	events := []store.ClickEvent{
		{LinkID: id, ClickedAt: now.Add(-1 * time.Hour)},
		{LinkID: id, ClickedAt: now.Add(-2 * time.Hour)},
	}
	if err := s.BatchInsertClicks(context.Background(), events); err != nil {
		t.Fatalf("insert clicks: %v", err)
	}

	rec := doRequest(e, authReq(http.MethodGet, fmt.Sprintf("/api/v1/links/%d/analytics?period=24h", id), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	result := parseBody(t, rec)
	if result["clicks_by_hour"] == nil {
		t.Error("expected clicks_by_hour in response")
	}
}

func TestIntegration_Analytics_InvalidPeriod(t *testing.T) {
	e, _ := setupTestServer(t)

	id, _ := createLink(t, e, "https://example.com/analyticsinv")

	rec := doRequest(e, authReq(http.MethodGet, fmt.Sprintf("/api/v1/links/%d/analytics?period=invalid", id), ""))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestIntegration_Analytics_NotFound(t *testing.T) {
	e, _ := setupTestServer(t)

	rec := doRequest(e, authReq(http.MethodGet, "/api/v1/links/99999/analytics", ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- QR Code ---

func TestIntegration_QRCode(t *testing.T) {
	e, _ := setupTestServer(t)

	id, _ := createLink(t, e, "https://example.com/qr")

	rec := doRequest(e, authReq(http.MethodGet, fmt.Sprintf("/api/v1/links/%d/qr", id), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("expected Content-Type image/png, got %q", ct)
	}
	// Check PNG magic bytes
	body := rec.Body.Bytes()
	if len(body) < 4 || body[0] != 0x89 || body[1] != 0x50 || body[2] != 0x4e || body[3] != 0x47 {
		t.Error("response is not a valid PNG")
	}
}

func TestIntegration_QRCode_CustomSize(t *testing.T) {
	e, _ := setupTestServer(t)

	id, _ := createLink(t, e, "https://example.com/qrsize")

	rec := doRequest(e, authReq(http.MethodGet, fmt.Sprintf("/api/v1/links/%d/qr?size=512", id), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.Bytes()
	if len(body) < 4 || body[0] != 0x89 {
		t.Error("response is not a valid PNG")
	}
}

func TestIntegration_QRCode_NotFound(t *testing.T) {
	e, _ := setupTestServer(t)

	rec := doRequest(e, authReq(http.MethodGet, "/api/v1/links/99999/qr", ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- Rate Limiting ---

func TestIntegration_RateLimit(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath, migrations.FS)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	cfg := &config.Config{
		BaseURL:           "http://localhost:8080",
		DefaultCodeLength: 6,
		MaxBulkURLs:       50,
		RateLimitEnabled:  true,
		APIKey:            testAPIKey,
	}

	e := echo.New()
	e.HideBanner = true
	e.HTTPErrorHandler = CustomHTTPErrorHandler

	limiterStore := NewRateLimiterStore(RateLimitConfig{Rate: 1, Burst: 2})

	linkHandler := NewLinkHandler(s, cfg)
	apiV1 := e.Group("/api/v1", AuthMiddleware(cfg.APIKey))
	apiV1.POST("/links", linkHandler.Create, RateLimitMiddleware(limiterStore, true))

	// Burst of 2: first 2 should succeed, 3rd should be rate limited
	var lastCode int
	for i := 0; i < 5; i++ {
		body := `{"url": "https://example.com"}`
		rec := doRequest(e, authReq(http.MethodPost, "/api/v1/links", body))
		lastCode = rec.Code
		if rec.Code == http.StatusTooManyRequests {
			// Check rate limit headers
			if rec.Header().Get("X-RateLimit-Limit") == "" {
				t.Error("expected X-RateLimit-Limit header")
			}
			if rec.Header().Get("Retry-After") == "" {
				t.Error("expected Retry-After header")
			}
			break
		}
	}

	if lastCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 eventually, last status was %d", lastCode)
	}
}

// --- Security Headers ---

func TestIntegration_SecurityHeaders(t *testing.T) {
	e, _ := setupTestServer(t)

	rec := doRequest(e, authReq(http.MethodGet, "/api/health", ""))
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("expected X-Content-Type-Options: nosniff")
	}
}

// --- Custom Error Handler ---

func TestIntegration_CustomErrorHandler_404(t *testing.T) {
	e, _ := setupTestServer(t)

	rec := doRequest(e, authReq(http.MethodGet, "/api/v1/nonexistent", ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	result := parseBody(t, rec)
	if result["error"] == nil {
		t.Error("expected error field in JSON response")
	}
}

func TestIntegration_BodyLimit(t *testing.T) {
	e, _ := setupTestServer(t)

	bigBody := strings.Repeat("x", 2*1024*1024) // 2MB
	req := authReq(http.MethodPost, "/api/v1/links", bigBody)
	rec := doRequest(e, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rec.Code)
	}

	result := parseBody(t, rec)
	if result["error"] != "request body too large" {
		t.Errorf("expected 'request body too large', got %v", result["error"])
	}
}

// --- CatchAll / Frontend middleware tests ---

// minimalFrontendHandler creates a FrontendHandler with minimal embedded content for testing.
func setupCatchAllServer(t *testing.T) (*echo.Echo, store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath, migrations.FS)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	cfg := &config.Config{
		BaseURL:           "http://localhost:8080",
		DefaultCodeLength: 6,
		MaxBulkURLs:       50,
		RateLimitEnabled:  false,
		APIKey:            testAPIKey,
	}

	recorder := clickrecorder.New(s, 1000, 60)

	e := echo.New()
	e.HideBanner = true
	e.HTTPErrorHandler = CustomHTTPErrorHandler
	e.Use(SecurityHeadersMiddleware())

	// API routes for creating links
	linkHandler := NewLinkHandler(s, cfg)
	apiV1 := e.Group("/api/v1", AuthMiddleware(cfg.APIKey))
	apiV1.POST("/links", linkHandler.Create)

	// Use a stubFrontendHandler that serves index for all fallbacks
	stubFrontend := &stubFrontendHandler{}

	redirectLimiter := NewRateLimiterStore(RateLimitRedirect)

	e.Use(CatchAllMiddleware(
		stubFrontend.asFrontendHandler(),
		s,
		cfg.BaseURL,
		redirectLimiter,
		false,
		recorder,
	))

	return e, s
}

// stubFrontendHandler satisfies the needs of CatchAllMiddleware.
type stubFrontendHandler struct{}

func (s *stubFrontendHandler) asFrontendHandler() *FrontendHandler {
	return &FrontendHandler{
		fileSystem: http.FS(os.DirFS(os.TempDir())),
		indexHTML:  []byte("<html>SPA</html>"),
	}
}

func TestIntegration_PublicQR(t *testing.T) {
	e, _ := setupCatchAllServer(t)

	// Create a link via API
	body := `{"url": "https://example.com/publicqr", "custom_code": "pubqr"}`
	rec := doRequest(e, authReq(http.MethodPost, "/api/v1/links", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create link failed: %d %s", rec.Code, rec.Body.String())
	}

	// Public QR (no auth)
	req := httptest.NewRequest(http.MethodGet, "/pubqr/qr", nil)
	rec = doRequest(e, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("expected Content-Type image/png, got %q", ct)
	}
	b := rec.Body.Bytes()
	if len(b) < 4 || b[0] != 0x89 {
		t.Error("not a valid PNG")
	}
}

func TestIntegration_PublicQR_NotFound(t *testing.T) {
	e, _ := setupCatchAllServer(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent/qr", nil)
	rec := doRequest(e, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	result := parseBody(t, rec)
	if result["error"] != "not found" {
		t.Errorf("expected 'not found' error, got %v", result["error"])
	}
}

func TestIntegration_PublicQR_MethodNotAllowed(t *testing.T) {
	e, _ := setupCatchAllServer(t)

	// Create a link
	body := `{"url": "https://example.com/qrmethod", "custom_code": "qrmeth"}`
	doRequest(e, authReq(http.MethodPost, "/api/v1/links", body))

	// POST to /:code/qr should fall through to SPA (not serve QR)
	req := httptest.NewRequest(http.MethodPost, "/qrmeth/qr", nil)
	rec := doRequest(e, req)
	// Should serve SPA index (200 with HTML)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 (SPA fallback), got %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("SPA")) {
		t.Error("expected SPA index content for non-GET QR request")
	}
}

func TestIntegration_Redirect_MethodCheck(t *testing.T) {
	e, _ := setupCatchAllServer(t)

	// Create a link
	body := `{"url": "https://example.com/redirect", "custom_code": "redir1"}`
	doRequest(e, authReq(http.MethodPost, "/api/v1/links", body))

	// POST to /:code should not redirect
	req := httptest.NewRequest(http.MethodPost, "/redir1", nil)
	rec := doRequest(e, req)
	if rec.Code == http.StatusFound {
		t.Error("POST to /:code should not trigger redirect")
	}
}

func TestIntegration_Redirect_GET(t *testing.T) {
	e, _ := setupCatchAllServer(t)

	body := `{"url": "https://example.com/target", "custom_code": "redir2"}`
	doRequest(e, authReq(http.MethodPost, "/api/v1/links", body))

	req := httptest.NewRequest(http.MethodGet, "/redir2", nil)
	rec := doRequest(e, req)
	if rec.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://example.com/target" {
		t.Errorf("expected redirect to target, got %q", loc)
	}
}

func TestIntegration_Redirect_Deactivated(t *testing.T) {
	e, s := setupCatchAllServer(t)

	body := `{"url": "https://example.com/deactivated", "custom_code": "deact1"}`
	rec := doRequest(e, authReq(http.MethodPost, "/api/v1/links", body))
	result := parseBody(t, rec)
	id := int64(result["id"].(float64))

	// Deactivate
	isActive := false
	s.UpdateLink(context.Background(), id, &isActive, nil)

	req := httptest.NewRequest(http.MethodGet, "/deact1", nil)
	rec = doRequest(e, req)
	if rec.Code != http.StatusGone {
		t.Errorf("expected 410, got %d", rec.Code)
	}
}

func TestIntegration_Redirect_Expired(t *testing.T) {
	e, s := setupCatchAllServer(t)

	body := `{"url": "https://example.com/expired", "custom_code": "exprd1"}`
	rec := doRequest(e, authReq(http.MethodPost, "/api/v1/links", body))
	result := parseBody(t, rec)
	id := int64(result["id"].(float64))

	// Set expired
	exp := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	s.UpdateLink(context.Background(), id, nil, &exp)

	req := httptest.NewRequest(http.MethodGet, "/exprd1", nil)
	rec = doRequest(e, req)
	if rec.Code != http.StatusGone {
		t.Errorf("expected 410, got %d", rec.Code)
	}
}
