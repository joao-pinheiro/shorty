package urlcheck

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const MaxURLLength = 2048

type SafeBrowsingChecker interface {
	Check(ctx context.Context, url string) error
}

var safeBrowsingChecker SafeBrowsingChecker

func SetSafeBrowsingChecker(c SafeBrowsingChecker) {
	safeBrowsingChecker = c
}

func Validate(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)

	if len(rawURL) > MaxURLLength {
		return fmt.Errorf("URL exceeds 2048 characters")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: must be http or https")
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("invalid URL: must be http or https")
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid URL: must be http or https")
	}

	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("URL flagged as potentially unsafe")
	}

	ip := net.ParseIP(host)
	if ip != nil {
		if isUnsafeIP(ip) {
			return fmt.Errorf("URL flagged as potentially unsafe")
		}
	}

	if safeBrowsingChecker != nil {
		if err := safeBrowsingChecker.Check(context.Background(), rawURL); err != nil {
			return fmt.Errorf("URL flagged as potentially unsafe")
		}
	}

	return nil
}

func isUnsafeIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}
