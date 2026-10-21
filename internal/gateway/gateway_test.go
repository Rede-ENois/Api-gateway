package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestGatewayAppliesRouteAuthAndRateLimit(t *testing.T) {
	g, err := New(Config{Workers: 2, QueueSize: 2, RateLimits: []RateLimitConfig{{Route: "/v1/models/**", Pattern: "fixed_window", Limit: 1, Window: Duration{Duration: time.Minute}, Key: "api_key"}}, Routes: []RouteConfig{{
		Name: "prediction", Pattern: "/v1/models/{model}/predict", Methods: []string{"POST"}, Upstream: "http://ml.example.test",
		Gatekeeper: GatekeeperConfig{Header: "X-Client-Token", APIKeys: []string{"demo-key"}, RequiredScopes: []string{"predict"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.clients.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/models/cat/predict" {
			t.Errorf("unexpected proxied path %q", r.URL.Path)
		}
		if r.Header.Get("X-Gateway-Route") != "prediction" {
			t.Errorf("missing trusted route header")
		}
		if r.Header.Get("X-Gateway-Client-IP") == "" {
			t.Errorf("missing trusted client IP header")
		}
		return &http.Response{StatusCode: http.StatusAccepted, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})

	bad := httptest.NewRequest(http.MethodPost, "/v1/models/cat/predict", nil)
	bad.Header.Set("X-Client-Token", "wrong")
	if rr := httptest.NewRecorder(); func() *httptest.ResponseRecorder { g.ServeHTTP(rr, bad); return rr }().Code != http.StatusForbidden {
		t.Fatal("expected forbidden")
	}

	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/models/cat/predict", nil)
		r.Header.Set("X-Client-Token", "demo-key")
		r.Header.Set("X-Scopes", "predict")
		rr := httptest.NewRecorder()
		g.ServeHTTP(rr, r)
		return rr
	}
	if got := request().Code; got != http.StatusAccepted {
		t.Fatalf("got %d", got)
	}
	if got := request().Code; got != http.StatusTooManyRequests {
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPatterns(t *testing.T) {
	for _, tc := range []struct {
		pattern, path string
		want          bool
	}{{"/v1/*", "/v1/a", true}, {"/v1/*", "/v1/a/b", false}, {"/users/**", "/users/a/b", true}, {"/users/**", "/users/123", true}, {"re:^/models/[0-9]+$", "/models/42", true}, {"/models/{id}", "/models/a", true}, {"/models/{id}", "/models/a/v", false}} {
		r, err := compileRoute(RouteConfig{Pattern: tc.pattern, Upstream: "http://example.test"})
		if err != nil {
			t.Fatal(err)
		}
		if got := r.matches(tc.path, "GET"); got != tc.want {
			t.Fatalf("%s %s: got %v", tc.pattern, tc.path, got)
		}
	}
}

func TestMoreSpecificRouteWinsOverWildcard(t *testing.T) {
	g, err := New(Config{Routes: []RouteConfig{{Name: "all-users", Pattern: "/users/**", Upstream: "http://wildcard.example.test"}, {Name: "profile", Pattern: "/users/me", Upstream: "http://profile.example.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	route, ok := g.lookup("/users/me", http.MethodGet)
	if !ok || route.config.Name != "profile" {
		t.Fatalf("expected the specific route, got %#v", route.config)
	}
}

func okResponse(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
}

func TestRateLimitedRequestReleasesConcurrencySlot(t *testing.T) {
	g, err := New(Config{
		RateLimits: []RateLimitConfig{{Route: "/x", Pattern: "fixed_window", Limit: 1, Window: Duration{Duration: time.Hour}, Key: "ip"}},
		Routes:     []RouteConfig{{Name: "x", Pattern: "/x", Upstream: "http://u.example.test", Gatekeeper: GatekeeperConfig{MaxConcurrentCalls: 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.clients.Transport = roundTripFunc(okResponse)
	for _, want := range []int{http.StatusOK, http.StatusTooManyRequests, http.StatusTooManyRequests} {
		rr := httptest.NewRecorder()
		g.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
		if rr.Code != want {
			t.Fatalf("got %d, want %d", rr.Code, want)
		}
	}
	if held := len(g.routes[0].gate.sem); held != 0 {
		t.Fatalf("%d concurrency slots held with no request in flight", held)
	}
}

func TestRequestCancelledWhileQueuedReturnsAndReleasesSlots(t *testing.T) {
	g, err := New(Config{Workers: 1, QueueSize: 1, Routes: []RouteConfig{{Name: "x", Pattern: "/x", Upstream: "http://u.example.test", Gatekeeper: GatekeeperConfig{MaxConcurrentCalls: 2}}}})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	started, unblock := make(chan struct{}), make(chan struct{})
	g.clients.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-unblock
		return okResponse(r)
	})
	first := make(chan struct{})
	go func() {
		g.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
		close(first)
	}()
	<-started

	// The only worker is busy, so this request waits in the queue until cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan struct{})
	go func() {
		g.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx))
		close(queued)
	}()
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
	g, err := New(Config{Workers: 1, QueueSize: 1, Routes: []RouteConfig{{Name: "x", Pattern: "/x", Upstream: "http://u.example.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	g.admitted <- struct{}{}
	g.admitted <- struct{}{}
	rr := httptest.NewRecorder()
	g.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", rr.Code)
	}
}
