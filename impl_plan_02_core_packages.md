# Phase 2: Core Packages — Implementation Plan

## Summary

Implement the `shortcode` and `urlcheck` packages as independently testable units. These have no dependencies on the store or HTTP layer. After this phase, both packages have full unit test coverage.

---

## Package 1: `backend/internal/shortcode/shortcode.go`

### Purpose

Generate cryptographically random short codes and validate custom aliases (S4).

### Constants and Variables

```go
package shortcode

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

const Charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// CustomCodeRegex validates custom aliases (S4: Custom Aliases).
var CustomCodeRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

// ReservedWords are rejected as custom codes (S4).
var ReservedWords = map[string]bool{
	"api":                        true,
	"health":                     true,
	"admin":                      true,
	"static":                     true,
	"assets":                     true,
	"favicon.ico":                true,
	"robots.txt":                 true,
	".well-known":                true,
	"sitemap.xml":                true,
	"manifest.json":              true,
	"sw.js":                      true,
	"apple-app-site-association": true,
}

const (
	MaxRetries       = 3 // retries per length
	DefaultLength    = 6
	EscalatedLength  = 7
)
```

### Function: `Generate`

```go
// Generate produces a cryptographically random code of the given length
// using charset [a-zA-Z0-9].
func Generate(length int) (string, error) {
	result := make([]byte, length)
	charsetLen := big.NewInt(int64(len(Charset)))
	for i := 0; i < length; i++ {
		idx, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("crypto/rand failed: %w", err)
		}
		result[i] = Charset[idx.Int64()]
	}
	return string(result), nil
}
```

### Function: `GenerateUnique`

This function handles the collision-retry logic (S4: Algorithm steps 1-5). It accepts a function to check existence in the DB.

```go
// ExistsFunc checks if a code already exists in the database.
type ExistsFunc func(code string) (bool, error)

// GenerateUnique generates a unique short code, retrying on collision.
// It tries up to MaxRetries times at defaultLength, then MaxRetries times
// at defaultLength+1. Returns error if all attempts collide.
func GenerateUnique(defaultLength int, exists ExistsFunc) (string, error) {
	// Try at default length
	for i := 0; i < MaxRetries; i++ {
		code, err := Generate(defaultLength)
		if err != nil {
			return "", err
		}
		taken, err := exists(code)
		if err != nil {
			return "", fmt.Errorf("checking code existence: %w", err)
		}
		if !taken {
			return code, nil
		}
	}

	// Escalate length by 1 and retry
	escalated := defaultLength + 1
	for i := 0; i < MaxRetries; i++ {
		code, err := Generate(escalated)
		if err != nil {
			return "", err
		}
		taken, err := exists(code)
		if err != nil {
			return "", fmt.Errorf("checking code existence: %w", err)
		}
		if !taken {
			return code, nil
		}
	}

	return "", fmt.Errorf("failed to generate unique code after %d attempts", MaxRetries*2)
}
```

### Function: `ValidateCustomCode`

```go
// ValidateCustomCode checks a user-supplied code against format and reserved word rules.
// Returns a human-readable error string or "" if valid.
func ValidateCustomCode(code string) string {
	if !CustomCodeRegex.MatchString(code) {
		return "code must be 3-32 alphanumeric, dash, or underscore"
	}
	if ReservedWords[strings.ToLower(code)] {
		return "code is reserved"
	}
	return ""
}
```

**Note**: The reserved word check is case-insensitive (lowercase comparison). The spec lists `api`, `favicon.ico`, etc. — `.well-known` contains a dot which is not in the custom code regex `[a-zA-Z0-9_-]`, so it would already be rejected by the regex. Same for `favicon.ico`, `robots.txt`, `sitemap.xml`, `manifest.json`, `sw.js`. Still, keep them in the reserved list for defense-in-depth.

---

## Package 1 Tests: `backend/internal/shortcode/shortcode_test.go`

### Test Cases

```go
package shortcode

import "testing"

func TestGenerate_Length(t *testing.T)
// Generate(6) returns a 6-char string. Generate(7) returns 7-char.

func TestGenerate_Charset(t *testing.T)
// Generate N codes, verify every character is in Charset.

func TestGenerate_Uniqueness(t *testing.T)
// Generate 10,000 codes of length 6, verify no duplicates (probabilistically safe).

func TestGenerateUnique_NoCollision(t *testing.T)
// ExistsFunc always returns false. Should succeed on first try.

func TestGenerateUnique_CollisionRetry(t *testing.T)
// ExistsFunc returns true for first 2 calls, then false. Should succeed on 3rd try.

func TestGenerateUnique_LengthEscalation(t *testing.T)
// ExistsFunc returns true for first 3 calls (all default-length attempts),
// then false on 4th call. Verify returned code has length defaultLength+1.

func TestGenerateUnique_Exhausted(t *testing.T)
// ExistsFunc always returns true. Should return error after 6 total attempts.

func TestValidateCustomCode_Valid(t *testing.T)
// "my-link", "abc", "a_b-c123", 32-char string → all return ""

func TestValidateCustomCode_TooShort(t *testing.T)
// "ab" → returns format error

func TestValidateCustomCode_TooLong(t *testing.T)
// 33-char string → returns format error

func TestValidateCustomCode_InvalidChars(t *testing.T)
// "my link", "my.link", "café" → returns format error

func TestValidateCustomCode_Reserved(t *testing.T)
// "api", "API", "health", "admin" → returns "code is reserved"
```

---

## Package 2: `backend/internal/urlcheck/urlcheck.go`

### Purpose

Multi-layer URL validation with SSRF prevention (S7).

```go
package urlcheck

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const MaxURLLength = 2048
```

### Function: `Validate`

```go
// Validate checks a URL against all validation rules (S7).
// Returns nil if valid, or an error with a user-facing message.
func Validate(rawURL string) error {
	// 0. Trim whitespace, normalize (S8.3)
	rawURL = strings.TrimSpace(rawURL)

	// 1. Length check (S7 step 4)
	if len(rawURL) > MaxURLLength {
		return fmt.Errorf("URL exceeds 2048 characters")
	}

	// 2. Parse (S7 step 1)
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: must be http or https")
	}

	// 3. Scheme check (S7 step 2)
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("invalid URL: must be http or https")
	}

	// 4. Host check (S7 step 3)
	host := u.Hostname() // strips port and brackets
	if host == "" {
		return fmt.Errorf("invalid URL: must be http or https")
	}

	// 4a. Reject localhost (case-insensitive)
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("URL flagged as potentially unsafe")
	}

	// 4b. If host is an IP literal, check for private/loopback/link-local
	ip := net.ParseIP(host)
	if ip != nil {
		if isUnsafeIP(ip) {
			return fmt.Errorf("URL flagged as potentially unsafe")
		}
	}

	return nil
}
```

### Function: `isUnsafeIP`

```go
// isUnsafeIP returns true if the IP is loopback, private, link-local, or unspecified.
// Covers IPv4, IPv6, and IPv6-mapped IPv4 (e.g., ::ffff:127.0.0.1) (S7 step 3).
func isUnsafeIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}
```

### Important Implementation Notes

- `net.ParseIP` handles IPv6-mapped IPv4 addresses like `::ffff:127.0.0.1`. Calling `IsLoopback()` on the parsed result correctly returns `true` for this case.
- `url.Parse` is lenient — a string like `"notaurl"` parses without error but has an empty scheme. The scheme check catches this.
- Hostnames other than `localhost` are NOT DNS-resolved at creation time (S7 step 3: "Hostnames other than localhost are not DNS-resolved at creation time").
- The error messages must match the spec exactly (S6.2 Errors table).

---

## Package 2 Tests: `backend/internal/urlcheck/urlcheck_test.go`

### Test Cases

```go
package urlcheck

import "testing"

func TestValidate_ValidURLs(t *testing.T)
// Table-driven test:
// - "https://example.com" → nil
// - "http://example.com/path?q=1" → nil
// - "https://example.com:8080/path" → nil
// - "https://sub.domain.example.com" → nil
// - "https://93.184.216.34/path" → nil (public IP)
// - "https://example.com/path%20with%20spaces" → nil (percent-encoded)
// - "https://example.com/unicode/日本語" → nil (Unicode, S19)

func TestValidate_InvalidScheme(t *testing.T)
// Table-driven:
// - "ftp://example.com" → error
// - "javascript:alert(1)" → error
// - "data:text/html,<h1>hi</h1>" → error
// - "" → error
// - "example.com" (no scheme) → error
// - "://missing-scheme" → error

func TestValidate_Localhost(t *testing.T)
// Table-driven:
// - "http://localhost" → error
// - "http://localhost:8080" → error
// - "http://LOCALHOST/path" → error (case-insensitive)
// - "http://LocalHost" → error

func TestValidate_PrivateIPs(t *testing.T)
// Table-driven:
// - "http://127.0.0.1" → error (loopback)
// - "http://127.0.0.1:8080" → error
// - "http://10.0.0.1" → error (private)
// - "http://172.16.0.1" → error (private)
// - "http://192.168.1.1" → error (private)
// - "http://0.0.0.0" → error (unspecified)

func TestValidate_IPv6(t *testing.T)
// Table-driven:
// - "http://[::1]" → error (loopback)
// - "http://[::1]:8080" → error
// - "http://[fe80::1]" → error (link-local)
// - "http://[::ffff:127.0.0.1]" → error (IPv6-mapped loopback)
// - "http://[::ffff:10.0.0.1]" → error (IPv6-mapped private)
// - "http://[::ffff:192.168.1.1]" → error (IPv6-mapped private)
// - "http://[::]" → error (unspecified)
// - "http://[2001:db8::1]" → nil (documentation address, but not blocked — only loopback/private/link-local/unspecified are blocked)

func TestValidate_TooLong(t *testing.T)
// URL of exactly 2048 chars → nil
// URL of 2049 chars → error "URL exceeds 2048 characters"

func TestValidate_EmptyHost(t *testing.T)
// "http://" → error
// "http:///path" → error

func TestValidate_WhitespaceTrimming(t *testing.T)
// "  https://example.com  " → nil (trimmed, S8.3)
```

### Test Structure

Use table-driven tests with `t.Run` for each case:

```go
func TestValidate_ValidURLs(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"simple https", "https://example.com"},
		{"with path and query", "http://example.com/path?q=1"},
		// ...
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.url); err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tc.url, err)
			}
		})
	}
}

func TestValidate_PrivateIPs(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"loopback v4", "http://127.0.0.1"},
		// ...
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.url)
			if err == nil {
				t.Errorf("Validate(%q) = nil, want error", tc.url)
			}
		})
	}
}
```

---

## Verification Checklist

1. Run `cd backend && go test ./internal/shortcode/... -race -count=1 -v` — all tests pass.
2. Run `cd backend && go test ./internal/urlcheck/... -race -count=1 -v` — all tests pass.
3. Run `cd backend && go vet ./...` — no issues.
4. Both packages have zero dependencies on `store`, `handler`, `config`, or `echo`.
5. `shortcode.Generate(6)` always returns exactly 6 characters from the charset `[a-zA-Z0-9]`.
6. `urlcheck.Validate("http://127.0.0.1")` returns an error.
7. `urlcheck.Validate("http://[::ffff:127.0.0.1]")` returns an error.
8. `urlcheck.Validate("http://localhost")` returns an error.
9. `urlcheck.Validate("https://example.com")` returns nil.
