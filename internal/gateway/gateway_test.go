package gateway

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func newTestGateway(t *testing.T, c Config) *Gateway {
	t.Helper()
	g, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	return g
}

func newUpstream(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

func serve(g *Gateway, r *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	g.ServeHTTP(rr, r)
	return rr
}

func okHandler(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }

func TestGatewayAppliesRouteAuthAndRateLimit(t *testing.T) {
	upstream := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/cat/predict" {
			t.Errorf("unexpected proxied path %q", r.URL.Path)
		}
		if r.Header.Get("X-Gateway-Route") != "prediction" {
			t.Errorf("missing trusted route header")
		}
		if r.Header.Get("X-Gateway-Client-IP") == "spoofed" {
			t.Errorf("client-provided X-Gateway-Client-IP reached the upstream")
		}
		w.WriteHeader(http.StatusAccepted)
	})
	g := newTestGateway(t, Config{Workers: 2, QueueSize: 2, RateLimits: []RateLimitConfig{{Route: "/v1/models/**", Pattern: "fixed_window", Limit: 1, Window: Duration{Duration: time.Minute}, Key: "api_key"}}, Routes: []RouteConfig{{
		Name: "prediction", Pattern: "/v1/models/{model}/predict", Methods: []string{"POST"}, Upstream: upstream,
		Gatekeeper: GatekeeperConfig{Header: "X-Client-Token", APIKeys: []string{"demo-key"}},
	}}})

	bad := httptest.NewRequest(http.MethodPost, "/v1/models/cat/predict", nil)
	bad.Header.Set("X-Client-Token", "wrong")
	if got := serve(g, bad).Code; got != http.StatusForbidden {
		t.Fatalf("got %d, want 403", got)
	}

	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/models/cat/predict", nil)
		r.Header.Set("X-Client-Token", "demo-key")
		r.Header.Set("X-Gateway-Client-IP", "spoofed")
		return r
	}
	if got := serve(g, request()).Code; got != http.StatusAccepted {
		t.Fatalf("got %d", got)
	}
	if got := serve(g, request()).Code; got != http.StatusTooManyRequests {
		t.Fatalf("got %d", got)
	}
}

func TestYAMLConfigAndDuration(t *testing.T) {
	var config Config
	err := yaml.Unmarshal([]byte("rate_limit:\n  - route: /users/**\n    pattern: token_bucket\n    limit: 10\n    window: 1m\nroutes:\n  - name: check\n    pattern: /check\n    upstream: https://ml.example.test\n    timeout: 5s\n    gatekeeper:\n      header: X-Client-Token\n      api_keys: [secret]\n"), &config)
	if err != nil {
		t.Fatal(err)
	}
	if config.Routes[0].Gatekeeper.Header != "X-Client-Token" || config.Routes[0].Timeout.Duration != 5*time.Second || config.RateLimits[0].Route != "/users/**" {
		t.Fatalf("YAML config was not decoded correctly: %#v", config.Routes[0])
	}
}

func TestPatterns(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{{"/v1/*", "/v1/a", true}, {"/v1/*", "/v1/a/b", false}, {"/users/**", "/users/a/b", true}, {"/users/**", "/users/123", true}, {"re:^/models/[0-9]+$", "/models/42", true}, {"/models/{id}", "/models/a", true}, {"/models/{id}", "/models/a/v", false}, {"/models/{model_id-2}", "/models/a", true}, {"/models/{1x}", "/models/{1x}", true}, {"/models/{1x}", "/models/a", false}} {
		m, err := compileMatcher(tc.pattern, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := m.matches(tc.path, "GET"); got != tc.want {
			t.Fatalf("%s %s: got %v", tc.pattern, tc.path, got)
		}
	}
}

func TestMostSpecificRouteWins(t *testing.T) {
	for _, tc := range []struct {
		patterns []string
		path     string
		want     string
	}{
		{[]string{"/users/**", "/users/me"}, "/users/me", "/users/me"},
		{[]string{"/users/{userIdentifier}", "/users/me"}, "/users/me", "/users/me"},
		{[]string{"/users/*", "/users/{id}"}, "/users/42", "/users/{id}"},
		{[]string{"/users/**", "/users/*"}, "/users/42", "/users/*"},
		{[]string{"/users/**", "/users/{id}/comments"}, "/users/42/comments", "/users/{id}/comments"},
		{[]string{"re:^/users/.*$", "/users/**"}, "/users/42", "/users/**"},
	} {
		var routes []RouteConfig
		for _, p := range tc.patterns {
			routes = append(routes, RouteConfig{Pattern: p, Upstream: "http://u.example.test"})
		}
		g := newTestGateway(t, Config{Routes: routes})
		route, ok := g.lookup(tc.path, http.MethodGet)
		if !ok || route.config.Pattern != tc.want {
			t.Errorf("%v on %s: got %q, want %q", tc.patterns, tc.path, route.config.Pattern, tc.want)
		}
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	route := func(timeout time.Duration) []RouteConfig {
		return []RouteConfig{{Pattern: "/x", Upstream: "http://u.example.test", Timeout: Duration{timeout}}}
	}
	for name, c := range map[string]Config{
		"unknown rate limit pattern": {RateLimits: []RateLimitConfig{{Route: "/x", Pattern: "fixed-window", Limit: 1}}},
		"unknown rate limit key":     {RateLimits: []RateLimitConfig{{Route: "/x", Limit: 1, Key: "token"}}},
		"zero rate limit":            {RateLimits: []RateLimitConfig{{Route: "/x"}}},
		"rate limit without route":   {RateLimits: []RateLimitConfig{{Limit: 1}}},
		"relative upstream":          {Routes: []RouteConfig{{Pattern: "/x", Upstream: "/relative"}}},
		"timeout over write timeout": {Routes: route(WriteTimeout)},
		"trusted proxy without mask": {TrustedProxies: []string{"10.0.0.1"}},
	} {
		if _, err := New(c); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestRateLimitedRequestReleasesConcurrencySlot(t *testing.T) {
	g := newTestGateway(t, Config{
		RateLimits: []RateLimitConfig{{Route: "/x", Pattern: "fixed_window", Limit: 1, Window: Duration{Duration: time.Hour}, Key: "ip"}},
		Routes:     []RouteConfig{{Name: "x", Pattern: "/x", Upstream: newUpstream(t, okHandler), Gatekeeper: GatekeeperConfig{MaxConcurrentCalls: 1}}},
	})
	for _, want := range []int{http.StatusOK, http.StatusTooManyRequests, http.StatusTooManyRequests} {
		if got := serve(g, httptest.NewRequest(http.MethodGet, "/x", nil)).Code; got != want {
			t.Fatalf("got %d, want %d", got, want)
		}
	}
	if held := len(g.routes[0].gate.sem); held != 0 {
		t.Fatalf("%d concurrency slots held with no request in flight", held)
	}
}

func TestRequestCancelledWhileQueuedReturnsAndReleasesSlots(t *testing.T) {
	started, unblock := make(chan struct{}), make(chan struct{})
	upstream := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-unblock
	})
	g := newTestGateway(t, Config{Workers: 1, QueueSize: 1, Routes: []RouteConfig{{Name: "x", Pattern: "/x", Upstream: upstream, Gatekeeper: GatekeeperConfig{MaxConcurrentCalls: 2}}}})
	first := make(chan struct{})
	go func() { serve(g, httptest.NewRequest(http.MethodGet, "/x", nil)); close(first) }()
	<-started

	// The only worker is busy, so this request waits in the queue until cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan struct{})
	go func() { serve(g, httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)); close(queued) }()
	cancel()
	select {
	case <-queued:
	case <-time.After(time.Second):
		t.Fatal("queued handler did not return after its client cancelled")
	}
	close(unblock)
	<-first
	if len(g.admitted) != 0 || len(g.running) != 0 || len(g.routes[0].gate.sem) != 0 {
		t.Fatalf("slots leaked: admitted=%d running=%d route=%d", len(g.admitted), len(g.running), len(g.routes[0].gate.sem))
	}
}

func TestFullQueueIsRejected(t *testing.T) {
	g := newTestGateway(t, Config{Workers: 1, QueueSize: 1, Routes: []RouteConfig{{Name: "x", Pattern: "/x", Upstream: "http://u.example.test"}}})
	g.admitted <- struct{}{}
	g.admitted <- struct{}{}
	if got := serve(g, httptest.NewRequest(http.MethodGet, "/x", nil)).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", got)
	}
}

func TestUpstreamErrorsMapToStatus(t *testing.T) {
	slow := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	echo := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		for {
			if _, err := r.Body.Read(buf); err != nil {
				return
			}
		}
	})
	g := newTestGateway(t, Config{MaxBodyBytes: 8, Routes: []RouteConfig{
		{Pattern: "/slow", Upstream: slow, Timeout: Duration{50 * time.Millisecond}},
		{Pattern: "/echo", Upstream: echo},
		{Pattern: "/down", Upstream: "http://127.0.0.1:1"},
	}})
	for _, tc := range []struct {
		request *http.Request
		want    int
	}{
		{httptest.NewRequest(http.MethodGet, "/slow", nil), http.StatusGatewayTimeout},
		{httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(strings.Repeat("x", 64))), http.StatusRequestEntityTooLarge},
		{httptest.NewRequest(http.MethodGet, "/down", nil), http.StatusBadGateway},
	} {
		if got := serve(g, tc.request).Code; got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.request.URL.Path, got, tc.want)
		}
	}
}

func TestClientIP(t *testing.T) {
	g := newTestGateway(t, Config{TrustedProxies: []string{"10.0.0.0/8"}})
	for _, tc := range []struct {
		name, remote, forwardedFor, want string
	}{
		{"untrusted peer ignores X-Forwarded-For", "203.0.113.9:1234", "198.51.100.1", "203.0.113.9"},
		{"trusted peer uses the forwarded client", "10.0.0.2:1234", "198.51.100.1", "198.51.100.1"},
		{"spoofed left hops are skipped", "10.0.0.2:1234", "1.2.3.4, 198.51.100.1, 10.0.0.3", "198.51.100.1"},
		{"trusted peer without header falls back to the peer", "10.0.0.2:1234", "", "10.0.0.2"},
		{"garbage hop falls back to the peer", "10.0.0.2:1234", "not-an-ip", "10.0.0.2"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tc.remote
		if tc.forwardedFor != "" {
			r.Header.Set("X-Forwarded-For", tc.forwardedFor)
		}
		if got := g.clientIP(r); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestOverloadLogIsSampled(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	g := newTestGateway(t, Config{})
	start := time.Unix(1000, 0)
	for _, at := range []time.Time{start, start, start.Add(time.Millisecond), start.Add(overloadLogInterval)} {
		g.logOverload(at)
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "rejected=1") || !strings.Contains(lines[1], "rejected=3") {
		t.Fatalf("want one line per interval carrying the rejection count, got:\n%s", logs.String())
	}
}
