# Phase 3: Middleware — Implementation Plan

## Summary

Implement authentication, rate limiting, structured request logging, CORS, security headers, body limit, panic recovery, and a custom error handler. After this phase, all middleware is wired into the Echo instance and unauthenticated API requests are rejected with 401.

---

## File: `backend/internal/handler/middleware.go`

### Auth Middleware (S5)

```go
package handler

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/time/rate"
)

// AuthMiddleware returns Echo middleware that validates the API key.
// It skips authentication for paths listed in skipPaths.
func AuthMiddleware(apiKey string) echo.MiddlewareFunc {
	apiKeyBytes := []byte(apiKey)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get("Authorization")
			if header == "" {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "missing authorization header",
				})
			}

			// Extract Bearer token
			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "invalid API key",
				})
			}
			token := header[len(prefix):]

			// Constant-time comparison (S5)
			if subtle.ConstantTimeCompare([]byte(token), apiKeyBytes) != 1 {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "invalid API key",
				})
			}

			return next(c)
		}
	}
}
```

#### Route Registration Strategy

Auth middleware is applied to the `/api/v1` group only. Public routes (`GET /:code`, `GET /:code/qr`, `GET /api/health`) are registered outside this group.

```go
// In main.go:
apiV1 := e.Group("/api/v1", handler.AuthMiddleware(cfg.APIKey))
// All /api/v1/* routes registered on apiV1 group
```

---

### Rate Limit Middleware (S8.1)

#### Rate Limit Configuration

```go
// RateLimitConfig defines rate/burst for a category of endpoints.
type RateLimitConfig struct {
	Rate  rate.Limit // requests per second (e.g., 10/min = 10.0/60.0)
	Burst int
}

// Predefined rate limits from S8.1.
var (
	RateLimitCreateLink = RateLimitConfig{Rate: rate.Limit(10.0 / 60.0), Burst: 20}
	RateLimitBulkCreate = RateLimitConfig{Rate: rate.Limit(2.0 / 60.0), Burst: 5}
	RateLimitRedirect   = RateLimitConfig{Rate: rate.Limit(100.0 / 60.0), Burst: 200}
	RateLimitDefault    = RateLimitConfig{Rate: rate.Limit(30.0 / 60.0), Burst: 60}
)
```

#### Per-IP Limiter Store

```go
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type RateLimiterStore struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiter
	config   RateLimitConfig
}

func NewRateLimiterStore(cfg RateLimitConfig) *RateLimiterStore {
	return &RateLimiterStore{
		limiters: make(map[string]*ipLimiter),
		config:   cfg,
	}
}

// GetLimiter returns the rate limiter for the given IP, creating one if needed.
func (s *RateLimiterStore) GetLimiter(ip string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.limiters[ip]; ok {
		entry.lastSeen = time.Now()
		return entry.limiter
	}

	limiter := rate.NewLimiter(s.config.Rate, s.config.Burst)
	s.limiters[ip] = &ipLimiter{limiter: limiter, lastSeen: time.Now()}
	return limiter
}

// Cleanup removes stale limiters (no activity for 10 minutes) (S8.1).
func (s *RateLimiterStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	for ip, entry := range s.limiters {
		if entry.lastSeen.Before(cutoff) {
			delete(s.limiters, ip)
		}
	}
}
```

#### Stale Limiter Cleanup Goroutine

Started in `main.go`:

```go
// StartCleanup runs cleanup every 5 minutes. Stopped via context cancellation.
func (s *RateLimiterStore) StartCleanup(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.Cleanup()
		case <-ctx.Done():
			return
		}
	}
}
```

#### Rate Limit Middleware Function

```go
// RateLimitMiddleware returns Echo middleware that enforces per-IP rate limits.
// When disabled (cfg.RateLimitEnabled == false), it is a no-op passthrough.
func RateLimitMiddleware(store *RateLimiterStore, enabled bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !enabled {
				return next(c)
			}

			ip := c.RealIP()
			limiter := store.GetLimiter(ip)

			reservation := limiter.Reserve()
			if !reservation.OK() {
				return rateLimitResponse(c, limiter)
			}

			if delay := reservation.Delay(); delay > 0 {
				reservation.Cancel()
				return rateLimitResponse(c, limiter)
			}

			// Set rate limit response headers (S8.1)
			setRateLimitHeaders(c, limiter, store.config)

			return next(c)
		}
	}
}

func rateLimitResponse(c echo.Context, limiter *rate.Limiter) error {
	retryAfter := int(60) // seconds
	c.Response().Header().Set("Retry-After", strconv.Itoa(retryAfter))
	c.Response().Header().Set("X-RateLimit-Limit", "0")
	c.Response().Header().Set("X-RateLimit-Remaining", "0")
	c.Response().Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Duration(retryAfter)*time.Second).Unix(), 10))
	return c.JSON(http.StatusTooManyRequests, map[string]interface{}{
		"error":       "rate limit exceeded",
		"retry_after": retryAfter,
	})
}

func setRateLimitHeaders(c echo.Context, limiter *rate.Limiter, cfg RateLimitConfig) {
	// Burst represents the limit
	limit := cfg.Burst
	// Tokens gives approximate remaining
	remaining := int(limiter.Tokens())
	if remaining < 0 {
		remaining = 0
	}
	// Reset: approximate time when fully replenished
	resetTime := time.Now().Add(time.Duration(float64(limit-remaining) / float64(cfg.Rate)) * time.Second)

	c.Response().Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
	c.Response().Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	c.Response().Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
}
```

#### IP Extraction with Trusted Proxies (S8.1)

Configure Echo's `IPExtractor` in `main.go`:

```go
import "github.com/labstack/echo/v4/middleware"

if len(cfg.TrustedProxies) > 0 {
	e.IPExtractor = echo.ExtractIPFromXFFHeader(
		// TrustOption: trust specified CIDRs
	)
} else {
	e.IPExtractor = echo.ExtractIPDirect()
}
```

The exact Echo API for trusted proxy configuration:

```go
// If TRUSTED_PROXIES is set, use X-Forwarded-For with trust ranges.
// If not set, use direct connection IP.
if len(cfg.TrustedProxies) > 0 {
	trustRanges := make([]net.IPNet, 0, len(cfg.TrustedProxies))
	for _, cidr := range cfg.TrustedProxies {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			slog.Error("invalid trusted proxy CIDR", "cidr", cidr, "error", err)
			os.Exit(1)
		}
		trustRanges = append(trustRanges, *ipNet)
	}
	e.IPExtractor = echo.ExtractIPFromXFFHeader(
		echo.TrustIPRange(trustRanges[0]),
		// Add more if needed — or use a loop
	)
}
```

**Note**: Check Echo v4's actual API. `echo.ExtractIPFromXFFHeader` may accept variadic `TrustOption`. If not, implement a custom `IPExtractor` function.

---

### Security Headers Middleware (S8.5)

```go
// SecurityHeadersMiddleware adds security headers to all responses.
func SecurityHeadersMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Set on all responses (S8.5)
			c.Response().Header().Set("X-Content-Type-Options", "nosniff")

			return next(c)
		}
	}
}
```

The `X-Frame-Options: DENY` header is added specifically in the redirect handler (Phase 4), not here, since it only applies to redirect responses (S8.5).

---

### Custom Error Handler (S13)

```go
// CustomHTTPErrorHandler ensures all errors return consistent JSON (S13).
func CustomHTTPErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}

	code := http.StatusInternalServerError
	message := "internal server error"

	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if m, ok := he.Message.(string); ok {
			message = m
		} else {
			message = http.StatusText(code)
		}
	}

	// Log 500 errors with full detail (S13)
	if code >= 500 {
		slog.Error("internal error", "error", err.Error(), "path", c.Request().URL.Path)
	}

	// Return JSON error (S13)
	c.JSON(code, map[string]string{"error": message})
}
```

Register in `main.go`:

```go
e.HTTPErrorHandler = handler.CustomHTTPErrorHandler
```

---

## File: Updated `backend/cmd/shorty/main.go`

### Request Logging Middleware (S12)

Configure structured JSON request logging via `slog`:

```go
// RequestLoggerMiddleware logs each request as structured JSON (S12).
func RequestLoggerMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()

			err := next(c)

			duration := time.Since(start)
			slog.Info("request",
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"status", c.Response().Status,
				"duration_ms", float64(duration.Microseconds())/1000.0,
				"ip", c.RealIP(),
			)

			return err
		}
	}
}
```

### Full Middleware Wiring in `main.go`

Order matters. Register in this sequence:

```go
import (
	echomw "github.com/labstack/echo/v4/middleware"
)

// 1. Recover from panics (S13)
e.Use(echomw.Recover())

// 2. Security headers (S8.5)
e.Use(handler.SecurityHeadersMiddleware())

// 3. Body limit (S8.2)
e.Use(echomw.BodyLimit("1M"))

// 4. CORS (S8.4)
e.Use(echomw.CORSWithConfig(echomw.CORSConfig{
	AllowOrigins: cfg.CORSAllowedOrigins,
	AllowMethods: []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	},
	AllowHeaders: []string{
		"Content-Type",
		"Authorization",
	},
	MaxAge: 86400,
}))

// 5. Request logging (S12)
e.Use(handler.RequestLoggerMiddleware())

// 6. Rate limiters — applied per route group, not globally
// Create separate stores for each endpoint category:
createLinkLimiter := handler.NewRateLimiterStore(handler.RateLimitCreateLink)
bulkCreateLimiter := handler.NewRateLimiterStore(handler.RateLimitBulkCreate)
redirectLimiter := handler.NewRateLimiterStore(handler.RateLimitRedirect)
defaultLimiter := handler.NewRateLimiterStore(handler.RateLimitDefault)

// Start cleanup goroutines for all limiter stores
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go createLinkLimiter.StartCleanup(ctx)
go bulkCreateLimiter.StartCleanup(ctx)
go redirectLimiter.StartCleanup(ctx)
go defaultLimiter.StartCleanup(ctx)

// 7. Custom error handler (S13)
e.HTTPErrorHandler = handler.CustomHTTPErrorHandler

// 8. Public routes (no auth)
e.GET("/api/health", healthHandler)
// /:code redirect and /:code/qr will be registered in Phase 4/12

// 9. Authenticated API group
apiV1 := e.Group("/api/v1", handler.AuthMiddleware(cfg.APIKey))

// Per-endpoint rate limiting applied via route-specific middleware:
apiV1.POST("/links",
	linksHandler.Create,
	handler.RateLimitMiddleware(createLinkLimiter, cfg.RateLimitEnabled),
)
apiV1.POST("/links/bulk",
	bulkHandler.Create,
	handler.RateLimitMiddleware(bulkCreateLimiter, cfg.RateLimitEnabled),
)
// Other endpoints use defaultLimiter:
apiV1.GET("/links",
	linksHandler.List,
	handler.RateLimitMiddleware(defaultLimiter, cfg.RateLimitEnabled),
)
// ... etc.
```

---

## Body Limit Error Handling (S8.2)

Echo's `BodyLimit` middleware returns a `413` error by default. The custom error handler (`CustomHTTPErrorHandler`) will format it as JSON. Verify that Echo's error message maps correctly, or override:

```go
// In CustomHTTPErrorHandler, handle 413 specifically:
if code == http.StatusRequestEntityTooLarge {
	message = "request body too large"
}
```

---

## Rate Limit 429 Response Format

Per S6.2 and S8.1, the 429 response must include:

```json
{"error": "rate limit exceeded", "retry_after": 60}
```

And the HTTP header `Retry-After: 60`.

The `rateLimitResponse` function handles both.

---

## CORS Configuration Details (S8.4)

The CORS middleware config must produce these headers:
- `Access-Control-Allow-Origin`: from `CORS_ALLOWED_ORIGINS` (default `http://localhost:5173`)
- `Access-Control-Allow-Methods: GET, POST, PATCH, DELETE, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type, Authorization`
- `Access-Control-Max-Age: 86400`

Echo's `CORSWithConfig` handles this directly with the config struct shown above.

---

## Imports Needed

Add `strconv` to `middleware.go` for header formatting:

```go
import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/time/rate"
)
```

---

## Verification Checklist

1. **Auth rejection**: `curl http://localhost:8080/api/v1/links` → `401 {"error": "missing authorization header"}`.
2. **Auth wrong key**: `curl -H "Authorization: Bearer wrong" http://localhost:8080/api/v1/links` → `401 {"error": "invalid API key"}`.
3. **Auth correct key**: `curl -H "Authorization: Bearer <correct>" http://localhost:8080/api/v1/links` → proceeds (404 or empty list, depending on handler registration).
4. **Health no auth**: `curl http://localhost:8080/api/health` → `200 {"status":"ok","version":"1.0.0"}`.
5. **Rate limiting**: Send 25 rapid `POST /api/v1/links` requests → after ~20, start getting `429` responses with `Retry-After` header and rate limit headers.
6. **Rate limit headers present**: Any successful request includes `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`.
7. **CORS preflight**: `curl -X OPTIONS -H "Origin: http://localhost:5173" -H "Access-Control-Request-Method: POST" http://localhost:8080/api/v1/links` → returns CORS headers.
8. **Security header**: Any response includes `X-Content-Type-Options: nosniff`.
9. **Body too large**: Send a >1MB POST body → `413 {"error": "request body too large"}`.
10. **Request logging**: Each request produces a structured JSON log line with method, path, status, duration_ms, ip.
11. **Error format**: `curl http://localhost:8080/nonexistent/api/path` → returns JSON `{"error": "..."}` not HTML.
12. **Rate limit disabled**: Set `RATE_LIMIT_ENABLED=false`, verify no 429 responses regardless of request volume.
