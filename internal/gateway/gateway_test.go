package gateway

import (
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
