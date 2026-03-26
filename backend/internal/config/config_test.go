package config

import (
	"testing"
)

func TestLoad_MissingAPIKey(t *testing.T) {
	t.Setenv("API_KEY", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing API_KEY")
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("PORT", "9090")
	t.Setenv("BASE_URL", "https://example.com/")
	t.Setenv("DB_PATH", "/tmp/test.db")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://a.com, http://b.com")
	t.Setenv("DEFAULT_CODE_LENGTH", "8")
	t.Setenv("MAX_BULK_URLS", "25")
	t.Setenv("CLICK_BUFFER_SIZE", "5000")
	t.Setenv("CLICK_FLUSH_INTERVAL", "2")
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 172.16.0.0/12")
	t.Setenv("DATA_RETENTION_DAYS", "90")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.BaseURL != "https://example.com" {
		t.Errorf("expected trailing slash stripped, got %q", cfg.BaseURL)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("expected /tmp/test.db, got %q", cfg.DBPath)
	}
	if cfg.APIKey != "test-key" {
		t.Errorf("expected test-key, got %q", cfg.APIKey)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected debug, got %q", cfg.LogLevel)
	}
	if len(cfg.CORSAllowedOrigins) != 2 || cfg.CORSAllowedOrigins[0] != "http://a.com" || cfg.CORSAllowedOrigins[1] != "http://b.com" {
		t.Errorf("unexpected CORS origins: %v", cfg.CORSAllowedOrigins)
	}
	if cfg.DefaultCodeLength != 8 {
		t.Errorf("expected 8, got %d", cfg.DefaultCodeLength)
	}
	if cfg.MaxBulkURLs != 25 {
		t.Errorf("expected 25, got %d", cfg.MaxBulkURLs)
	}
	if cfg.ClickBufferSize != 5000 {
		t.Errorf("expected 5000, got %d", cfg.ClickBufferSize)
	}
	if cfg.ClickFlushInterval != 2 {
		t.Errorf("expected 2, got %d", cfg.ClickFlushInterval)
	}
	if cfg.RateLimitEnabled {
		t.Error("expected rate limit disabled")
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0] != "10.0.0.0/8" {
		t.Errorf("unexpected trusted proxies: %v", cfg.TrustedProxies)
	}
	if cfg.DataRetentionDays != 90 {
		t.Errorf("expected 90, got %d", cfg.DataRetentionDays)
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("PORT", "")
	t.Setenv("BASE_URL", "")
	t.Setenv("DB_PATH", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("DEFAULT_CODE_LENGTH", "")
	t.Setenv("MAX_BULK_URLS", "")
	t.Setenv("CLICK_BUFFER_SIZE", "")
	t.Setenv("CLICK_FLUSH_INTERVAL", "")
	t.Setenv("RATE_LIMIT_ENABLED", "")
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("DATA_RETENTION_DAYS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("expected default base URL, got %q", cfg.BaseURL)
	}
	if cfg.DefaultCodeLength != 6 {
		t.Errorf("expected default code length 6, got %d", cfg.DefaultCodeLength)
	}
	if cfg.MaxBulkURLs != 50 {
		t.Errorf("expected default max bulk 50, got %d", cfg.MaxBulkURLs)
	}
	if cfg.ClickBufferSize != 10000 {
		t.Errorf("expected default buffer 10000, got %d", cfg.ClickBufferSize)
	}
	if !cfg.RateLimitEnabled {
		t.Error("expected rate limit enabled by default")
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("PORT", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid PORT")
	}
}

func TestLoad_InvalidDefaultCodeLength(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("DEFAULT_CODE_LENGTH", "abc")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid DEFAULT_CODE_LENGTH")
	}
}

func TestLoad_InvalidRateLimitEnabled(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("RATE_LIMIT_ENABLED", "maybe")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid RATE_LIMIT_ENABLED")
	}
}

func TestLoad_InvalidClickBufferSize(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("CLICK_BUFFER_SIZE", "xyz")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid CLICK_BUFFER_SIZE")
	}
}

func TestLoad_InvalidMaxBulkURLs(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("MAX_BULK_URLS", "xyz")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid MAX_BULK_URLS")
	}
}

func TestLoad_InvalidClickFlushInterval(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("CLICK_FLUSH_INTERVAL", "xyz")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid CLICK_FLUSH_INTERVAL")
	}
}

func TestLoad_InvalidDataRetentionDays(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("DATA_RETENTION_DAYS", "xyz")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid DATA_RETENTION_DAYS")
	}
}

func TestLoad_BaseURLTrailingSlashStripped(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("BASE_URL", "https://example.com///")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "https://example.com" {
		t.Errorf("expected trailing slashes stripped, got %q", cfg.BaseURL)
	}
}

func TestLoad_TrustedProxiesSplitting(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("TRUSTED_PROXIES", " 10.0.0.0/8 , 172.16.0.0/12 , 192.168.0.0/16 ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TrustedProxies) != 3 {
		t.Errorf("expected 3 proxies, got %d: %v", len(cfg.TrustedProxies), cfg.TrustedProxies)
	}
}

func TestLoad_EmptyTrustedProxies(t *testing.T) {
	t.Setenv("API_KEY", "test-key")
	t.Setenv("TRUSTED_PROXIES", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Errorf("expected empty proxies, got %v", cfg.TrustedProxies)
	}
}
