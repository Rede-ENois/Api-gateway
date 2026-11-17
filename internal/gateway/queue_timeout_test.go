package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueuedRequestRespectsRouteTimeout(t *testing.T) {
	called := make(chan struct{}, 1)
	upstream := newUpstream(t, func(_ http.ResponseWriter, _ *http.Request) {
		called <- struct{}{}
	})
	g := newTestGateway(t, Config{Workers: 1, QueueSize: 1, Routes: []RouteConfig{{
		Name: "x", Pattern: "/x", Upstream: upstream, Timeout: Duration{time.Nanosecond},
		Gatekeeper: GatekeeperConfig{MaxConcurrentCalls: 1},
	}}})
	g.running <- struct{}{}
	defer func() { <-g.running }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got := serve(g, httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)).Code
	if got != http.StatusGatewayTimeout {
		t.Fatalf("got %d, want %d", got, http.StatusGatewayTimeout)
	}
	select {
	case <-called:
		t.Fatal("upstream called while worker was occupied")
	default:
	}
	if len(g.admitted) != 0 || len(g.running) != 1 || len(g.routes[0].gate.sem) != 0 {
		t.Fatalf("unexpected slots: admitted=%d running=%d route=%d", len(g.admitted), len(g.running), len(g.routes[0].gate.sem))
	}
}
