package httpx

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func rejectDial(t *testing.T) func(context.Context, string, string) (net.Conn, error) {
	t.Helper()
	return func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("default client dialed")
		return nil, errors.New("dialed")
	}
}

func TestClientRejectsPrivateLoopbackAndMetadata(t *testing.T) {
	client := New()
	if client.PermitLoopback {
		t.Fatal("production client permits loopback")
	}
	client.dial = rejectDial(t)
	client.Resolver = resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
		switch host {
		case "private.example":
			return []net.IPAddr{{IP: net.ParseIP("10.1.2.3")}}, nil
		case "meta.example":
			return []net.IPAddr{{IP: net.ParseIP("169.254.169.254")}}, nil
		case "link.example":
			return []net.IPAddr{{IP: net.ParseIP("169.254.1.5")}}, nil
		case "loop.example":
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		default:
			t.Fatalf("unexpected lookup %s", host)
			return nil, errors.New("unexpected")
		}
	})
	ctx := context.Background()
	for _, raw := range []string{
		"http://10.1.2.3/",
		"http://169.254.169.254/latest",
		"http://169.254.1.5/",
		"http://127.0.0.1/",
		"http://[::1]/",
		"http://[::ffff:169.254.169.254]/",
		"http://private.example/",
		"http://meta.example/latest",
		"http://link.example/",
		"http://loop.example/",
		"http://0.0.0.0/",
		"http://224.0.0.1/",
		"file:///etc/passwd",
		"http://user:secret@example.com/",
	} {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Do(ctx, req); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestClientRejectsRedirectToMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer srv.Close()
	client := New()
	client.PermitLoopback = true
	client.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			t.Errorf("dialed non-loopback %s", addr)
			return nil, errors.New("拒绝访问该地址")
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), req); err == nil || !strings.Contains(err.Error(), "拒绝") {
		t.Fatalf("redirect err = %v", err)
	}
}

func TestClientStripsAuthorizationOnCrossHostRedirect(t *testing.T) {
	var got string
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/landed", http.StatusFound)
	}))
	defer first.Close()
	client := New()
	client.PermitLoopback = true
	req, err := http.NewRequest(http.MethodGet, first.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := client.Do(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(resp.Body) != "ok" {
		t.Fatalf("status %d body %s", resp.StatusCode, resp.Body)
	}
	if got != "" {
		t.Fatalf("authorization forwarded: %q", got)
	}
}

func TestClientRedirectLimitAndBodyCap(t *testing.T) {
	var hops int
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		if hops < 5 {
			http.Redirect(w, r, srv.URL+"/", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "done")
	}))
	defer srv.Close()
	client := New()
	client.PermitLoopback = true
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(context.Background(), req); err == nil {
		t.Fatal("followed a fourth redirect")
	}

	bodySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello world")
	}))
	defer bodySrv.Close()
	limited := New()
	limited.PermitLoopback = true
	limited.MaxBytes = 4
	req, err = http.NewRequest(http.MethodGet, bodySrv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := limited.Do(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Truncated || string(resp.Body) != "hell" {
		t.Fatalf("truncated=%v body=%q", resp.Truncated, resp.Body)
	}
	if New().MaxBytes != 2<<20 || New().Timeout != 10*time.Second || New().MaxRedirects != 3 {
		t.Fatal("default limits changed")
	}
}

func TestRetryDelay(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	h := make(http.Header)
	if _, ok := RetryDelay(h, now); ok {
		t.Fatal("missing header reported a delay")
	}
	h.Set("Retry-After", "12")
	d, ok := RetryDelay(h, now)
	if !ok || d != 12*time.Second {
		t.Fatalf("retry-after %s %v", d, ok)
	}
	h.Del("Retry-After")
	h.Set("X-RateLimit-Reset", "1700000000")
	if _, ok := RetryDelay(h, now); !ok {
		t.Fatal("x-ratelimit-reset ignored")
	}
}

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}
