package urlcheck

import (
	"strings"
	"testing"
)

func TestValidate_ValidURLs(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"simple https", "https://example.com"},
		{"http with path and query", "http://example.com/path?q=1"},
		{"https with port", "https://example.com:8080/path"},
		{"subdomain", "https://sub.domain.example.com"},
		{"public IP", "https://93.184.216.34/path"},
		{"percent-encoded", "https://example.com/path%20with%20spaces"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.url); err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tc.url, err)
			}
		})
	}
}

func TestValidate_InvalidScheme(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"ftp", "ftp://example.com"},
		{"javascript", "javascript:alert(1)"},
		{"data", "data:text/html,<h1>hi</h1>"},
		{"empty", ""},
		{"no scheme", "example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.url); err == nil {
				t.Errorf("Validate(%q) = nil, want error", tc.url)
			}
		})
	}
}

func TestValidate_Localhost(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"localhost", "http://localhost"},
		{"localhost with port", "http://localhost:8080"},
		{"LOCALHOST", "http://LOCALHOST/path"},
		{"mixed case", "http://LocalHost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.url); err == nil {
				t.Errorf("Validate(%q) = nil, want error", tc.url)
			}
		})
	}
}

func TestValidate_PrivateIPs(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"loopback", "http://127.0.0.1"},
		{"loopback with port", "http://127.0.0.1:8080"},
		{"private 10.x", "http://10.0.0.1"},
		{"private 172.16.x", "http://172.16.0.1"},
		{"private 192.168.x", "http://192.168.1.1"},
		{"unspecified", "http://0.0.0.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.url); err == nil {
				t.Errorf("Validate(%q) = nil, want error", tc.url)
			}
		})
	}
}

func TestValidate_IPv6(t *testing.T) {
	blocked := []struct {
		name string
		url  string
	}{
		{"loopback", "http://[::1]"},
		{"loopback with port", "http://[::1]:8080"},
		{"link-local", "http://[fe80::1]"},
		{"mapped loopback", "http://[::ffff:127.0.0.1]"},
		{"mapped private 10.x", "http://[::ffff:10.0.0.1]"},
		{"mapped private 192.168.x", "http://[::ffff:192.168.1.1]"},
		{"unspecified", "http://[::]"},
	}
	for _, tc := range blocked {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.url); err == nil {
				t.Errorf("Validate(%q) = nil, want error", tc.url)
			}
		})
	}

	// Public IPv6 should be allowed
	t.Run("public ipv6", func(t *testing.T) {
		if err := Validate("http://[2001:db8::1]"); err != nil {
			t.Errorf("Validate(public ipv6) = %v, want nil", err)
		}
	})
}

func TestValidate_TooLong(t *testing.T) {
	base := "https://example.com/"
	// Exactly 2048
	exact := base + strings.Repeat("a", MaxURLLength-len(base))
	if err := Validate(exact); err != nil {
		t.Errorf("URL of exactly %d chars rejected: %v", MaxURLLength, err)
	}

	// 2049
	tooLong := exact + "a"
	if err := Validate(tooLong); err == nil {
		t.Error("URL of 2049 chars accepted, want error")
	}
}

func TestValidate_EmptyHost(t *testing.T) {
	cases := []string{"http://", "http:///path"}
	for _, u := range cases {
		if err := Validate(u); err == nil {
			t.Errorf("Validate(%q) = nil, want error", u)
		}
	}
}

func TestValidate_WhitespaceTrimming(t *testing.T) {
	if err := Validate("  https://example.com  "); err != nil {
		t.Errorf("Validate with whitespace = %v, want nil", err)
	}
}
