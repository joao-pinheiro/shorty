package handler

import (
	"context"
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

// AuthMiddleware returns Echo middleware that validates the API key.
// Applied to the /api/v1 group; public routes are registered outside that group.
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

			const prefix = "Bearer "
			if !strings.HasPrefix(header, prefix) {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "invalid API key",
				})
			}
			token := header[len(prefix):]

			if subtle.ConstantTimeCompare([]byte(token), apiKeyBytes) != 1 {
				return c.JSON(http.StatusUnauthorized, map[string]string{
					"error": "invalid API key",
				})
			}

			return next(c)
		}
	}
}

// RateLimitConfig defines rate/burst for a category of endpoints.
type RateLimitConfig struct {
	Rate  rate.Limit
	Burst int
}

var (
	RateLimitCreateLink = RateLimitConfig{Rate: rate.Limit(10.0 / 60.0), Burst: 20}
	RateLimitBulkCreate = RateLimitConfig{Rate: rate.Limit(2.0 / 60.0), Burst: 5}
	RateLimitRedirect   = RateLimitConfig{Rate: rate.Limit(100.0 / 60.0), Burst: 200}
	RateLimitDefault    = RateLimitConfig{Rate: rate.Limit(30.0 / 60.0), Burst: 60}
)

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiterStore holds per-IP rate limiters for a single endpoint category.
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

// Cleanup removes stale limiters (no activity for 10 minutes).
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

// StartCleanup runs Cleanup every 5 minutes until ctx is cancelled.
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

// RateLimitMiddleware enforces per-IP rate limits. No-op when enabled is false.
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
				return rateLimitResponse(c, limiter, store.config)
			}

			if delay := reservation.Delay(); delay > 0 {
				reservation.Cancel()
				return rateLimitResponse(c, limiter, store.config)
			}

			setRateLimitHeaders(c, limiter, store.config)
			return next(c)
		}
	}
}

func rateLimitResponse(c echo.Context, limiter *rate.Limiter, cfg RateLimitConfig) error {
	ratePerMinute := float64(cfg.Rate) * 60
	retryAfter := int(60 / ratePerMinute)
	if retryAfter < 1 {
		retryAfter = 1
	}
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
	limit := cfg.Burst
	remaining := int(limiter.Tokens())
	if remaining < 0 {
		remaining = 0
	}
	resetTime := time.Now().Add(time.Duration(float64(limit-remaining)/float64(cfg.Rate)) * time.Second)

	c.Response().Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
	c.Response().Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	c.Response().Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
}

// SecurityHeadersMiddleware adds security headers to all responses.
func SecurityHeadersMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("X-Content-Type-Options", "nosniff")
			return next(c)
		}
	}
}

// RequestLoggerMiddleware logs each request as structured JSON via slog.
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

// CustomHTTPErrorHandler ensures all errors return consistent JSON.
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

	if code == http.StatusRequestEntityTooLarge {
		message = "request body too large"
	}

	if code >= 500 {
		slog.Error("internal error", "error", err.Error(), "path", c.Request().URL.Path)
	}

	_ = c.JSON(code, map[string]string{"error": message})
}
