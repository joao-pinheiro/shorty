package shortcode

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

const Charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var CustomCodeRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

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
	MaxRetries = 3
)

type ExistsFunc func(code string) (bool, error)

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

func GenerateUnique(defaultLength int, exists ExistsFunc) (string, error) {
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

func ValidateCustomCode(code string) string {
	if !CustomCodeRegex.MatchString(code) {
		return "code must be 3-32 alphanumeric, dash, or underscore"
	}
	if ReservedWords[strings.ToLower(code)] {
		return "code is reserved"
	}
	return ""
}
