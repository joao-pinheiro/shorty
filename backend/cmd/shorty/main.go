package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"

	"shorty/internal/config"
	"shorty/internal/handler"
	"shorty/internal/migrations"
	"shorty/internal/store"
)

var migrateOnly = flag.Bool("migrate", false, "Run migrations and exit")

func main() {
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Configure structured JSON logging
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	db, err := store.New(cfg.DBPath, migrations.FS)
	if err != nil {
		slog.Error("failed to open store", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if *migrateOnly {
		slog.Info("migrations applied successfully")
		return
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Custom error handler (S13)
	e.HTTPErrorHandler = handler.CustomHTTPErrorHandler

	// IP extraction with trusted proxies (S8.1)
	if len(cfg.TrustedProxies) > 0 {
		var trustOptions []echo.TrustOption
		for _, cidr := range cfg.TrustedProxies {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				slog.Error("invalid trusted proxy CIDR", "cidr", cidr, "error", err)
				os.Exit(1)
			}
			trustOptions = append(trustOptions, echo.TrustIPRange(ipNet))
		}
		e.IPExtractor = echo.ExtractIPFromXFFHeader(trustOptions...)
	}

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

	// 6. Rate limiter stores — per endpoint category
	createLinkLimiter := handler.NewRateLimiterStore(handler.RateLimitCreateLink)
	bulkCreateLimiter := handler.NewRateLimiterStore(handler.RateLimitBulkCreate)
	redirectLimiter := handler.NewRateLimiterStore(handler.RateLimitRedirect)
	defaultLimiter := handler.NewRateLimiterStore(handler.RateLimitDefault)

	// Start cleanup goroutines
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	go createLinkLimiter.StartCleanup(cleanupCtx)
	go bulkCreateLimiter.StartCleanup(cleanupCtx)
	go redirectLimiter.StartCleanup(cleanupCtx)
	go defaultLimiter.StartCleanup(cleanupCtx)

	// Create handlers
	linkHandler := handler.NewLinkHandler(db, cfg)
	redirectHandler := handler.NewRedirectHandler(db)

	// Public routes — no auth required
	e.GET("/api/health", handler.HealthCheck)

	// Authenticated API group (S5)
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

	// Suppress unused variable — will be used in later phases
	_ = bulkCreateLimiter

	// Redirect route (no auth, rate limited)
	e.GET("/:code", redirectHandler.Redirect,
		handler.RateLimitMiddleware(redirectLimiter, cfg.RateLimitEnabled))

	addr := fmt.Sprintf(":%d", cfg.Port)
	slog.Info("starting server", "addr", addr, "base_url", cfg.BaseURL)

	go func() {
		if err := e.Start(addr); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown (S10)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cleanupCancel()

	if err := e.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
