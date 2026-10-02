package catalog

import (
	"net"
	"net/url"
	"strings"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

// CanonicalURL is the shared urlcanon function. It lowercases the host,
// drops default ports and the fragment, and removes known tracking parameters.
// It does not upgrade http to https.
func CanonicalURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return "", badURL()
	}
	if parsed.User != nil {
		return "", badURL()
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", badURL()
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "", badURL()
	}
	if ip := net.ParseIP(host); ip != nil && blockedIP(ip) {
		return "", badURL()
	}
	port := parsed.Port()
	if port != "" && !defaultPort(scheme, port) {
		host = net.JoinHostPort(host, port)
	}
	query := parsed.Query()
	for key := range query {
		if dropQuery(key) {
			query.Del(key)
		}
	}
	out := url.URL{
		Scheme:   scheme,
		Host:     host,
		Path:     parsed.Path,
		RawPath:  parsed.RawPath,
		RawQuery: query.Encode(),
	}
	return out.String(), nil
}

func badURL() error {
	return apperr.Invalid("网址不正确", apperr.FieldError{Field: "website_url", Code: "invalid"})
}

func defaultPort(scheme, port string) bool {
	return (scheme == "http" && port == "80") || (scheme == "https" && port == "443")
}

func dropQuery(key string) bool {
	switch strings.ToLower(key) {
	case "fbclid", "gclid", "ref", "mc_cid", "mc_eid":
		return true
	default:
		return strings.HasPrefix(strings.ToLower(key), "utm_")
	}
}

func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
