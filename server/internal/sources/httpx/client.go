package httpx

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout   = 10 * time.Second
	defaultMaxBytes  = 2 << 20
	defaultRedirects = 3
)

// Resolver looks up a hostname before connect. Tests supply a fake.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Client fetches http(s) URLs and refuses private, loopback, link-local, and metadata addresses.
// PermitLoopback is a test-only option. Production code must leave it false.
type Client struct {
	SameOriginOnly bool
	PermitLoopback bool
	Resolver       Resolver
	Timeout        time.Duration
	MaxBytes       int64
	MaxRedirects   int
	dial           func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Response is a bounded body. Truncated is set when the body hit the cap.
type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Truncated  bool
	FinalURL   string
}

func New() *Client {
	return &Client{Timeout: defaultTimeout, MaxBytes: defaultMaxBytes, MaxRedirects: defaultRedirects}
}

// Do performs the request. Redirects are checked before connect. Authorization is dropped on a host change.
func (c *Client) Do(ctx context.Context, req *http.Request) (*Response, error) {
	if c == nil {
		c = New()
	}
	if req == nil || req.URL == nil {
		return nil, errors.New("请求不正确")
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	current := req.Clone(ctx)
	redirects := 0
	for {
		if err := validateURL(current.URL); err != nil {
			return nil, err
		}
		ips, err := c.resolve(ctx, current.URL.Hostname())
		if err != nil {
			return nil, err
		}
		if err := c.vet(ips); err != nil {
			return nil, err
		}
		resp, err := c.roundTrip(ctx, current, ips[0])
		if err != nil {
			return nil, err
		}
		if !isRedirect(resp.StatusCode) || redirects >= c.maxRedirects() {
			body, truncated, readErr := readLimit(resp.Body, c.maxBytes())
			resp.Body.Close()
			if readErr != nil {
				return nil, readErr
			}
			if isRedirect(resp.StatusCode) && redirects >= c.maxRedirects() {
				return nil, errors.New("重定向次数过多")
			}
			return &Response{
				StatusCode: resp.StatusCode,
				Header:     resp.Header.Clone(),
				Body:       body,
				Truncated:  truncated,
				FinalURL:   current.URL.String(),
			}, nil
		}
		loc, err := resp.Location()
		resp.Body.Close()
		if err != nil {
			return nil, errors.New("重定向地址不正确")
		}
		redirects++
		nextURL := current.URL.ResolveReference(loc)
		if c.SameOriginOnly && !sameSite(current.URL, nextURL) {
			return nil, errors.New("拒绝跨来源重定向")
		}
		if err := validateURL(nextURL); err != nil {
			return nil, err
		}
		nextIPs, err := c.resolve(ctx, nextURL.Hostname())
		if err != nil {
			return nil, err
		}
		if err := c.vet(nextIPs); err != nil {
			return nil, err
		}
		next := current.Clone(ctx)
		next.URL = nextURL
		next.Host = nextURL.Host
		if !sameSite(current.URL, nextURL) {
			next.Header.Del("Authorization")
		}
		current = next
	}
}

// RetryDelay reads Retry-After or X-RateLimit-Reset. ok is false when neither header is present.
func RetryDelay(h http.Header, now time.Time) (time.Duration, bool) {
	if h == nil {
		return 0, false
	}
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if secs, err := atoiNonNeg(v); err == nil {
			return time.Duration(secs) * time.Second, true
		}
		if when, err := http.ParseTime(v); err == nil {
			d := when.Sub(now)
			if d < 0 {
				d = 0
			}
			return d, true
		}
	}
	if v := strings.TrimSpace(h.Get("X-RateLimit-Reset")); v != "" {
		sec, err := parseInt(v)
		if err != nil {
			return 0, false
		}
		d := time.Unix(sec, 0).Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

func (c *Client) roundTrip(ctx context.Context, req *http.Request, ip net.IP) (*http.Response, error) {
	port := req.URL.Port()
	if port == "" {
		if req.URL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	addr := net.JoinHostPort(ip.String(), port)
	transport := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			if c.blocked(ip) {
				return nil, errors.New("拒绝访问该地址")
			}
			if c.dial != nil {
				return c.dial(ctx, network, addr)
			}
			return (&net.Dialer{Timeout: c.timeout()}).DialContext(ctx, network, addr)
		},
		TLSClientConfig:       &tls.Config{ServerName: req.URL.Hostname(), MinVersion: tls.VersionTLS12},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: c.timeout(),
		ForceAttemptHTTP2:     false,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   c.timeout(),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client.Do(req)
}

func (c *Client) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{normalizeIP(ip)}, nil
	}
	resolver := c.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return nil, errors.New("无法解析主机名")
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP == nil {
			continue
		}
		ips = append(ips, normalizeIP(addr.IP))
	}
	if len(ips) == 0 {
		return nil, errors.New("无法解析主机名")
	}
	return ips, nil
}

func (c *Client) vet(ips []net.IP) error {
	if len(ips) == 0 {
		return errors.New("无法解析主机名")
	}
	for _, ip := range ips {
		if c.blocked(ip) {
			return errors.New("拒绝访问该地址")
		}
	}
	return nil
}

func (c *Client) blocked(ip net.IP) bool {
	ip = normalizeIP(ip)
	if ip == nil {
		return true
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}
	if ip.IsLoopback() {
		return !c.PermitLoopback
	}
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

func normalizeIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

func validateURL(u *url.URL) error {
	if u == nil {
		return errors.New("网址不正确")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return errors.New("只允许 http 或 https")
	}
	if u.User != nil || u.Hostname() == "" {
		return errors.New("网址不正确")
	}
	return nil
}

func isRedirect(code int) bool {
	return code == http.StatusMovedPermanently || code == http.StatusFound || code == http.StatusSeeOther || code == http.StatusTemporaryRedirect || code == http.StatusPermanentRedirect
}

func sameSite(a, b *url.URL) bool {
	return strings.EqualFold(a.Hostname(), b.Hostname()) && a.Scheme == b.Scheme && a.Port() == b.Port()
}

func readLimit(r io.Reader, max int64) ([]byte, bool, error) {
	if max <= 0 {
		max = defaultMaxBytes
	}
	buf, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > max {
		return buf[:max], true, nil
	}
	return buf, false, nil
}

func (c *Client) timeout() time.Duration {
	if c == nil || c.Timeout <= 0 {
		return defaultTimeout
	}
	return c.Timeout
}

func (c *Client) maxBytes() int64 {
	if c == nil || c.MaxBytes <= 0 {
		return defaultMaxBytes
	}
	return c.MaxBytes
}

func (c *Client) maxRedirects() int {
	if c == nil || c.MaxRedirects <= 0 {
		return defaultRedirects
	}
	return c.MaxRedirects
}

func atoiNonNeg(s string) (int, error) {
	n, err := parseInt(s)
	if err != nil || n < 0 {
		return 0, errors.New("bad")
	}
	return int(n), nil
}

func parseInt(s string) (int64, error) {
	var n int64
	if s == "" {
		return 0, errors.New("bad")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("bad")
		}
		n = n*10 + int64(r-'0')
	}
	return n, nil
}
