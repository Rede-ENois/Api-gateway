package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Gateway composes route matching, admission control, rate limiting and proxying.
type Gateway struct {
	routes     []runtimeRoute
	rateLimits []runtimeRateLimit
	pool       *Pool
	maxBody    int64
	clients    *http.Client
	closed     sync.Once
}

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
	g := &Gateway{pool: NewPool(c.Workers, c.QueueSize), maxBody: c.MaxBodyBytes, clients: &http.Client{Transport: &http.Transport{MaxIdleConns: 256, MaxIdleConnsPerHost: 64, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ExpectContinueTimeout: 1 * time.Second}}}
	for _, rc := range c.Routes {
		r, err := compileRoute(rc)
		if err != nil {
			g.Close()
			return nil, err
		}
		u, err := url.Parse(rc.Upstream)
		if err != nil || u.Scheme == "" || u.Host == "" {
			g.Close()
			return nil, errors.New("route " + rc.Name + ": upstream must be an absolute URL")
		}
		r.config.Upstream = u.String()
		g.routes = append(g.routes, runtimeRoute{route: r, gate: newGatekeeper(r.config.Gatekeeper)})
	}
	for _, lc := range c.RateLimits {
		if lc.Route == "" {
			g.Close()
			return nil, errors.New("rate limit needs a route pattern")
		}
		policyRoute, err := compileRoute(RouteConfig{Name: lc.Route, Pattern: lc.Route, Upstream: "http://rate-limit.invalid"})
		if err != nil {
			g.Close()
			return nil, err
		}
		if makeLimiter(lc) == nil {
			g.Close()
			return nil, errors.New("rate limit " + lc.Route + " needs a positive limit")
		}
		g.rateLimits = append(g.rateLimits, runtimeRateLimit{route: policyRoute, config: lc, limiter: makeLimiter(lc)})
	}
	return g, nil
}

func (g *Gateway) Close() { g.closed.Do(func() { g.pool.Close(); g.clients.CloseIdleConnections() }) }

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
	if !route.gate.acquire() {
		http.Error(w, "too many concurrent requests", http.StatusTooManyRequests)
		return
	}
	if policy, found := g.lookupRateLimit(r.URL.Path, r.Method); found {
		allowed, retry := policy.limiter.Allow(limitKey(policy.config.Key, route.gate.header, r), time.Now())
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
	done := make(chan struct{})
	if err := g.pool.Submit(r.Context(), func() { defer close(done); defer route.gate.release(); g.proxy(w, r, route) }); err != nil {
		route.gate.release()
		w.Header().Set("Retry-After", "1")
		http.Error(w, "gateway overloaded", http.StatusServiceUnavailable)
		return
	}
	<-done
}

type runtimeRoute struct {
	route
	gate gatekeeper
}

type runtimeRateLimit struct {
	route
	config  RateLimitConfig
	limiter limiter
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

// Gatekeeper checks the API key and scope before scarce worker capacity is used.
type gatekeeper struct {
	keys   map[string]struct{}
	header string
	scopes map[string]struct{}
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
	scopes := map[string]struct{}{}
	for _, s := range c.RequiredScopes {
		scopes[s] = struct{}{}
	}
	var sem chan struct{}
	if c.MaxConcurrentCalls > 0 {
		sem = make(chan struct{}, c.MaxConcurrentCalls)
	}
	return gatekeeper{keys: keys, header: header, scopes: scopes, sem: sem}
}
func (g gatekeeper) authorized(r *http.Request) bool {
	if len(g.keys) > 0 {
		if _, ok := g.keys[r.Header.Get(g.header)]; !ok {
			return false
		}
	}
	if len(g.scopes) > 0 {
		have := map[string]struct{}{}
		for _, s := range strings.Fields(r.Header.Get("X-Scopes")) {
			have[s] = struct{}{}
		}
		for s := range g.scopes {
			if _, ok := have[s]; !ok {
				return false
			}
		}
	}
	return true
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

func (g *Gateway) proxy(w http.ResponseWriter, r *http.Request, rt runtimeRoute) {
	target, _ := url.Parse(rt.config.Upstream)
	ctx := r.Context()
	timeout := rt.config.Timeout.Duration
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()
	clone := r.Clone(ctx)
	clone.RequestURI = ""
	clone.URL.Scheme = target.Scheme
	clone.URL.Host = target.Host
	clone.URL.Path = joinURLPath(target.Path, r.URL.Path)
	clone.Host = target.Host
	clone.Header.Del("Connection")
	clone.Header.Del("Proxy-Connection")
	clone.Header.Del("X-Gateway-Client-IP")
	clone.Header.Del("X-Gateway-Route")
	clone.Header.Set("X-Forwarded-For", clientIP(r))
	clone.Header.Set("X-Forwarded-Proto", requestScheme(r))
	// Trusted metadata lets the moderation model associate a verdict with the
	// source without accepting spoofed client-provided gateway headers.
	clone.Header.Set("X-Gateway-Client-IP", clientIP(r))
	clone.Header.Set("X-Gateway-Route", rt.config.Name)
	resp, err := g.clients.Do(clone)
	if err != nil {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func joinURLPath(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
func limitKey(kind, credentialHeader string, r *http.Request) string {
	ip, key := clientIP(r), r.Header.Get(credentialHeader)
	switch kind {
	case "ip":
		return ip
	case "api_key":
		return key
	default:
		return ip + ":" + key
	}
}
