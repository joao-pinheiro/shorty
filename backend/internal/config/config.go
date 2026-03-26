package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                  int
	BaseURL               string
	DBPath                string
	APIKey                string
	LogLevel              string
	CORSAllowedOrigins    []string
	DefaultCodeLength     int
	MaxBulkURLs           int
	ClickBufferSize       int
	ClickFlushInterval    int
	RateLimitEnabled      bool
	TrustedProxies        []string
	GoogleSafeBrowsingKey string
	DataRetentionDays     int
}

func Load() (*Config, error) {
	// Load .env if present; ignore if missing
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		// godotenv returns its own error type for missing files
		if !os.IsNotExist(err) {
			// Only log, don't fail — .env is optional
		}
	}

	apiKey := strings.TrimSpace(os.Getenv("API_KEY"))
	if apiKey == "" {
		return nil, fmt.Errorf("API_KEY environment variable is required")
	}

	port, err := envIntOrDefault("PORT", 8080)
	if err != nil {
		return nil, fmt.Errorf("invalid PORT: %w", err)
	}

	defaultCodeLength, err := envIntOrDefault("DEFAULT_CODE_LENGTH", 6)
	if err != nil {
		return nil, fmt.Errorf("invalid DEFAULT_CODE_LENGTH: %w", err)
	}

	maxBulkURLs, err := envIntOrDefault("MAX_BULK_URLS", 50)
	if err != nil {
		return nil, fmt.Errorf("invalid MAX_BULK_URLS: %w", err)
	}

	clickBufferSize, err := envIntOrDefault("CLICK_BUFFER_SIZE", 10000)
	if err != nil {
		return nil, fmt.Errorf("invalid CLICK_BUFFER_SIZE: %w", err)
	}

	clickFlushInterval, err := envIntOrDefault("CLICK_FLUSH_INTERVAL", 1)
	if err != nil {
		return nil, fmt.Errorf("invalid CLICK_FLUSH_INTERVAL: %w", err)
	}

	dataRetentionDays, err := envIntOrDefault("DATA_RETENTION_DAYS", 0)
	if err != nil {
		return nil, fmt.Errorf("invalid DATA_RETENTION_DAYS: %w", err)
	}

	rateLimitEnabled := true
	if v := os.Getenv("RATE_LIMIT_ENABLED"); v != "" {
		rateLimitEnabled, err = strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid RATE_LIMIT_ENABLED: %w", err)
		}
	}

	baseURL := strings.TrimRight(envOrDefault("BASE_URL", "http://localhost:8080"), "/")

	return &Config{
		Port:                  port,
		BaseURL:               baseURL,
		DBPath:                envOrDefault("DB_PATH", "./shorty.db"),
		APIKey:                apiKey,
		LogLevel:              envOrDefault("LOG_LEVEL", "info"),
		CORSAllowedOrigins:    splitAndTrim(envOrDefault("CORS_ALLOWED_ORIGINS", "http://localhost:5173")),
		DefaultCodeLength:     defaultCodeLength,
		MaxBulkURLs:           maxBulkURLs,
		ClickBufferSize:       clickBufferSize,
		ClickFlushInterval:    clickFlushInterval,
		RateLimitEnabled:      rateLimitEnabled,
		TrustedProxies:        splitAndTrim(os.Getenv("TRUSTED_PROXIES")),
		GoogleSafeBrowsingKey: os.Getenv("GOOGLE_SAFE_BROWSING_API_KEY"),
		DataRetentionDays:     dataRetentionDays,
	}, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envIntOrDefault(key string, defaultVal int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal, nil
	}
	return strconv.Atoi(v)
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
