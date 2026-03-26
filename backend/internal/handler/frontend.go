package handler

import (
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"shorty/internal/clickrecorder"
	"shorty/internal/qr"
	"shorty/internal/store"
)

// FrontendHandler serves embedded frontend static files and provides SPA fallback.
type FrontendHandler struct {
	fileSystem http.FileSystem
	indexHTML  []byte
}

// NewFrontendHandler creates a handler from the embedded FS.
// distFS should be the embed.FS, subDir is the prefix to strip (e.g., "dist").
func NewFrontendHandler(embeddedFS fs.FS, subDir string) (*FrontendHandler, error) {
	subFS, err := fs.Sub(embeddedFS, subDir)
	if err != nil {
		return nil, fmt.Errorf("fs.Sub(%s): %w", subDir, err)
	}

	indexHTML, err := fs.ReadFile(subFS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read index.html: %w", err)
	}

	return &FrontendHandler{
		fileSystem: http.FS(subFS),
		indexHTML:  indexHTML,
	}, nil
}

// HasStaticFile checks if the given path corresponds to a file in the embedded filesystem.
func (fh *FrontendHandler) HasStaticFile(path string) bool {
	name := strings.TrimPrefix(path, "/")
	if name == "" || name == "index.html" {
		return false
	}

	f, err := fh.fileSystem.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return !stat.IsDir()
}

// ServeStaticFile serves a static asset from the embedded filesystem.
func (fh *FrontendHandler) ServeStaticFile(c echo.Context) error {
	http.FileServer(fh.fileSystem).ServeHTTP(c.Response(), c.Request())
	return nil
}

// ServeIndex serves index.html for SPA client-side routing.
func (fh *FrontendHandler) ServeIndex(c echo.Context) error {
	return c.HTMLBlob(http.StatusOK, fh.indexHTML)
}

// CatchAllMiddleware returns an Echo middleware that handles all non-API requests.
// It checks in order:
//  1. Static file in embedded frontend -> serve it
//  2. Path matches /:code/qr -> serve public QR code (no auth)
//  3. Path matches a short code in DB -> 302 redirect
//  4. Fallback -> serve index.html (SPA routing)
func CatchAllMiddleware(
	frontend *FrontendHandler,
	s store.Store,
	baseURL string,
	rl *RateLimiterStore,
	rateLimitEnabled bool,
	recorder *clickrecorder.Recorder,
) echo.MiddlewareFunc {
	qrPathRegex := regexp.MustCompile(`^/([a-zA-Z0-9_-]{3,32})/qr$`)
	codeRegex := regexp.MustCompile(`^/([a-zA-Z0-9_-]{3,32})$`)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path

			// Skip /api/* paths — let Echo's registered routes handle them
			if strings.HasPrefix(path, "/api/") || path == "/api" {
				return next(c)
			}

			// Normalize: strip trailing slash (S19)
			if len(path) > 1 && strings.HasSuffix(path, "/") {
				path = strings.TrimRight(path, "/")
			}

			// 1. Check for static file in embedded frontend
			if frontend.HasStaticFile(path) {
				return frontend.ServeStaticFile(c)
			}

			// 2. Check /:code/qr pattern (S6.9 — public, no auth)
			if matches := qrPathRegex.FindStringSubmatch(path); matches != nil {
				code := matches[1]

				if c.Request().Method != http.MethodGet {
					return frontend.ServeIndex(c)
				}

				// Rate limit (S8.1: redirect + public QR: 100/min, burst 200)
				if rateLimitEnabled {
					limiter := rl.GetLimiter(c.RealIP())
					reservation := limiter.Reserve()
					if !reservation.OK() {
						return rateLimitResponse(c, limiter, rl.config)
					}
					if delay := reservation.Delay(); delay > 0 {
						reservation.Cancel()
						return rateLimitResponse(c, limiter, rl.config)
					}
					setRateLimitHeaders(c, limiter, rl.config)
				}

				link, err := s.GetLinkByCode(c.Request().Context(), code)
				if err != nil {
					return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
				}

				size := 256
				if sizeStr := c.QueryParam("size"); sizeStr != "" {
					parsed, err := strconv.Atoi(sizeStr)
					if err == nil && parsed >= 128 && parsed <= 1024 {
						size = parsed
					}
				}

				shortURL := baseURL + "/" + link.Code
				png, err := qr.Generate(shortURL, size)
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate QR code")
				}

				return c.Blob(http.StatusOK, "image/png", png)
			}

			// 3. Check if path matches a short code -> redirect (S6.1)
			if matches := codeRegex.FindStringSubmatch(path); matches != nil {
				code := matches[1]

				if c.Request().Method != http.MethodGet {
					return frontend.ServeIndex(c)
				}

				// Rate limit (S8.1: 100/min, burst 200)
				if rateLimitEnabled {
					limiter := rl.GetLimiter(c.RealIP())
					reservation := limiter.Reserve()
					if !reservation.OK() {
						return rateLimitResponse(c, limiter, rl.config)
					}
					if delay := reservation.Delay(); delay > 0 {
						reservation.Cancel()
						return rateLimitResponse(c, limiter, rl.config)
					}
					setRateLimitHeaders(c, limiter, rl.config)
				}

				link, err := s.GetLinkByCode(c.Request().Context(), code)
				if err != nil {
					// Not a known short code -> fall through to SPA fallback
					return frontend.ServeIndex(c)
				}

				// Check active/expired status (S6.1)
				if !link.IsActive {
					return c.JSON(http.StatusGone, map[string]string{
						"error": "link is deactivated",
					})
				}

				if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now().UTC()) {
					_ = s.DeactivateExpiredLink(c.Request().Context(), link.ID)
					return c.JSON(http.StatusGone, map[string]string{
						"error": "link has expired",
					})
				}

				// Record click
				recorder.Record(link.ID)

				// Security headers for redirect (S8.5)
				c.Response().Header().Set("X-Frame-Options", "DENY")
				c.Response().Header().Set("Cache-Control", "private, max-age=0")

				return c.Redirect(http.StatusFound, link.OriginalURL)
			}

			// 4. Fallback: serve index.html for SPA routing (S15.5)
			return frontend.ServeIndex(c)
		}
	}
}
