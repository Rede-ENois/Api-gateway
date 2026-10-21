package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Gateway composes route matching, admission control, rate limiting and proxying.
type Gateway struct {
	routes     []runtimeRoute
	rateLimits []runtimeRateLimit
	// admitted bounds requests running or waiting for a worker (workers +
	// queue_size); running bounds concurrent upstream calls (workers).
	admitted       chan struct{}
	running        chan struct{}
	maxBody        int64
	transport      *http.Transport
	trustedProxies []netip.Prefix
}

// WriteTimeout is the server write timeout. Route timeouts must stay below it,
// or the server cuts the response before the upstream call times out.
const WriteTimeout = 60 * time.Second

const defaultRouteTimeout = 30 * time.Second

func New(c Config) (*Gateway, error) {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.Workers <= 0 {
		c.Workers = 64
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 512
	}
	if c.MaxBodyBytes <= 0 {
		c.MaxBodyBytes = 10 << 20
	}
	g := &Gateway{admitted: make(chan struct{}, c.Workers+c.QueueSize), running: make(chan struct{}, c.Workers), maxBody: c.MaxBodyBytes, transport: &http.Transport{MaxIdleConns: 256, MaxIdleConnsPerHost: 64, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ExpectContinueTimeout: 1 * time.Second}}
	for _, cidr := range c.TrustedProxies {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("trusted proxy %q must be a CIDR such as 10.0.0.0/8: %w", cidr, err)
		}
		g.trustedProxies = append(g.trustedProxies, prefix.Masked())
	}
	for _, rc := range c.Routes {
		r, err := compileRoute(rc)
		if err != nil {
			g.Close()
			return nil, err
		}
		target, err := url.Parse(rc.Upstream)
		if err != nil || target.Scheme == "" || target.Host == "" {
			g.Close()
			return nil, fmt.Errorf("route %q: upstream must be an absolute URL", r.config.Name)
		}
		timeout := rc.Timeout.Duration
		if timeout <= 0 {
			timeout = defaultRouteTimeout
		}
		if timeout >= WriteTimeout {
			g.Close()
			return nil, fmt.Errorf("route %q: timeout must be below the server write timeout (%s)", r.config.Name, WriteTimeout)
		}
		if len(rc.Gatekeeper.APIKeys) == 0 {
			slog.Warn("route has no api_keys and is public", "route", r.config.Name)
		}
		g.routes = append(g.routes, runtimeRoute{route: r, gate: newGatekeeper(rc.Gatekeeper), timeout: timeout, proxy: g.newProxy(target, r.config.Name)})
	}
	for _, lc := range c.RateLimits {
		if lc.Route == "" {
			g.Close()
			return nil, errors.New("rate limit needs a route pattern")
		}
		policyMatcher, err := compileMatcher(lc.Route, nil)
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("rate limit %q: %w", lc.Route, err)
		}
		switch lc.Key {
		case "", "ip", "api_key", "ip_api_key":
		default:
			g.Close()
			return nil, fmt.Errorf("rate limit %q: unknown key %q (want ip, api_key or ip_api_key)", lc.Route, lc.Key)
		}
		policyLimiter, err := makeLimiter(lc)
		if err != nil {
			g.Close()
			return nil, fmt.Errorf("rate limit %q: %w", lc.Route, err)
		}
		g.rateLimits = append(g.rateLimits, runtimeRateLimit{matcher: policyMatcher, config: lc, limiter: policyLimiter})
	}
	return g, nil
}

func (g *Gateway) Close() { g.transport.CloseIdleConnections() }

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
		return
	}
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, g.maxBody)
	}
	route, ok := g.lookup(r.URL.Path, r.Method)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !route.gate.authorized(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if policy, found := g.lookupRateLimit(r.URL.Path, r.Method); found {
		allowed, retry := policy.limiter.Allow(limitKey(policy.config.Key, g.clientIP(r), r.Header.Get(route.gate.header)), time.Now())
		if !allowed {
			seconds := int(retry.Seconds())
			if retry%time.Second != 0 {
				seconds++
			}
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}
	if !route.gate.acquire() {
		http.Error(w, "too many concurrent requests", http.StatusTooManyRequests)
		return
	}
	defer route.gate.release()
	select {
	case g.admitted <- struct{}{}:
		defer func() { <-g.admitted }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "gateway overloaded", http.StatusServiceUnavailable)
		return
	}
	select {
	case g.running <- struct{}{}:
		defer func() { <-g.running }()
	case <-r.Context().Done():
		return // the client gave up while waiting for a worker
	}
	ctx, cancel := context.WithTimeout(r.Context(), route.timeout)
	defer cancel()
	route.proxy.ServeHTTP(w, r.WithContext(ctx))
}

type runtimeRoute struct {
	route
	gate    gatekeeper
	timeout time.Duration
	proxy   *httputil.ReverseProxy
}

type runtimeRateLimit struct {
	matcher
	config  RateLimitConfig
	limiter *keyedLimiter
}

func (g *Gateway) lookup(path, method string) (runtimeRoute, bool) {
	var best runtimeRoute
	found := false
	for _, r := range g.routes {
		if r.matches(path, method) {
			if !found || r.specificity > best.specificity {
				best, found = r, true
			}
		}
	}
	return best, found
}

func (g *Gateway) lookupRateLimit(path, method string) (runtimeRateLimit, bool) {
	var best runtimeRateLimit
	found := false
	for _, policy := range g.rateLimits {
		if policy.matches(path, method) && (!found || policy.specificity > best.specificity) {
			best, found = policy, true
		}
	}
	return best, found
}

// Gatekeeper checks the API key before scarce worker capacity is used.
type gatekeeper struct {
	keys   map[string]struct{}
	header string
	sem    chan struct{}
}

func newGatekeeper(c GatekeeperConfig) gatekeeper {
	keys := map[string]struct{}{}
	for _, k := range c.APIKeys {
		keys[k] = struct{}{}
	}
	header := c.Header
	if header == "" {
		header = "X-API-Key"
	}
	var sem chan struct{}
	if c.MaxConcurrentCalls > 0 {
		sem = make(chan struct{}, c.MaxConcurrentCalls)
	}
	return gatekeeper{keys: keys, header: header, sem: sem}
}
func (g gatekeeper) authorized(r *http.Request) bool {
	if len(g.keys) == 0 {
		return true
	}
	_, ok := g.keys[r.Header.Get(g.header)]
	return ok
}
func (g gatekeeper) acquire() bool {
	if g.sem == nil {
		return true
	}
	select {
	case g.sem <- struct{}{}:
		return true
	default:
		return false
	}
}
func (g gatekeeper) release() {
	if g.sem != nil {
		<-g.sem
	}
}

func (g *Gateway) newProxy(target *url.URL, routeName string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Transport: g.transport,
		// ReverseProxy has already stripped hop-by-hop and X-Forwarded-* headers.
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			ip := g.clientIP(pr.In)
			pr.Out.Header.Set("X-Forwarded-For", ip)
			pr.Out.Header.Set("X-Forwarded-Proto", requestScheme(pr.In))
			// Trusted metadata lets the moderation model associate a verdict with the
			// source; Set replaces any spoofed copy the client sent.
			pr.Out.Header.Set("X-Gateway-Client-IP", ip)
			pr.Out.Header.Set("X-Gateway-Route", routeName)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			status := http.StatusBadGateway
			var tooLarge *http.MaxBytesError
			switch {
			case errors.As(err, &tooLarge):
				status = http.StatusRequestEntityTooLarge
			case errors.Is(r.Context().Err(), context.DeadlineExceeded):
				status = http.StatusGatewayTimeout
			case r.Context().Err() != nil:
				return // the client went away; nobody reads the response
			}
			slog.Error("upstream request failed", "route", routeName, "status", status, "error", err)
			http.Error(w, http.StatusText(status), status)
		},
	}
}

// clientIP returns the peer address or, when the peer is a trusted proxy, the
// rightmost X-Forwarded-For hop that is not a trusted proxy. Hops to the left
// of it were written by the client and cannot be trusted.
func (g *Gateway) clientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if !g.isTrustedProxy(ip) {
		return ip
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if g.isTrustedProxy(hop) {
			continue
		}
		if _, err := netip.ParseAddr(hop); err != nil {
			return ip
		}
		return hop
	}
	return ip
}

func (g *Gateway) isTrustedProxy(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range g.trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
func limitKey(kind, ip, key string) string {
	switch kind {
	case "ip":
		return ip
	case "api_key":
		return key
	default:
		return ip + ":" + key
	}
}
