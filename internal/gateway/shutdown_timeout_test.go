package gateway

import (
	"testing"
	"time"
)

func TestShutdownTimeout(t *testing.T) {
	route := func(pattern string, timeout time.Duration) RouteConfig {
		return RouteConfig{Pattern: pattern, Upstream: "http://u.example.test", Timeout: Duration{timeout}}
	}
	for _, tc := range []struct {
		name   string
		routes []RouteConfig
		want   time.Duration
	}{
		{"no routes", nil, 31 * time.Second},
		{"short route", []RouteConfig{route("/x", 5*time.Second)}, 6 * time.Second},
		{"default route", []RouteConfig{route("/x", 5*time.Second), route("/y", 0)}, 31 * time.Second},
		{"negative timeout uses default", []RouteConfig{route("/x", -time.Second)}, 31 * time.Second},
		{"largest route", []RouteConfig{route("/x", 45*time.Second), route("/y", 25*time.Second)}, 46 * time.Second},
		{"capped at write timeout", []RouteConfig{route("/x", WriteTimeout-time.Millisecond)}, WriteTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newTestGateway(t, Config{Routes: tc.routes})
			if got := g.ShutdownTimeout(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
