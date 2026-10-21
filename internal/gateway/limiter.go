package gateway

import (
	"math"
	"sync"
	"time"
)

type limiter interface {
	Allow(string, time.Time) (bool, time.Duration)
}
type limiterEntry struct {
	l    limiter
	seen time.Time
}
type keyedLimiter struct {
	make   func() limiter
	mu     sync.Mutex
	values map[string]limiterEntry
}

func newKeyedLimiter(makeFn func() limiter) *keyedLimiter {
	return &keyedLimiter{make: makeFn, values: map[string]limiterEntry{}}
}
func (k *keyedLimiter) Allow(key string, now time.Time) (bool, time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e, ok := k.values[key]
	if !ok {
		e.l = k.make()
	}
	yes, retry := e.l.Allow(key, now)
	e.seen = now
	k.values[key] = e
	return yes, retry
}

type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	rate   float64
	burst  float64
}

func (t *tokenBucket) Allow(_ string, now time.Time) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last.IsZero() {
		t.last = now
		t.tokens = t.burst
	}
	t.tokens = math.Min(t.burst, t.tokens+now.Sub(t.last).Seconds()*t.rate)
	t.last = now
	if t.tokens >= 1 {
		t.tokens--
		return true, 0
	}
	return false, time.Duration(math.Ceil((1 - t.tokens) / t.rate * float64(time.Second)))
}

type fixedWindow struct {
	mu           sync.Mutex
	start        time.Time
	count, limit int
	window       time.Duration
}

func (f *fixedWindow) Allow(_ string, now time.Time) (bool, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.start.IsZero() || now.Sub(f.start) >= f.window {
		f.start = now
		f.count = 0
	}
	if f.count < f.limit {
		f.count++
		return true, 0
	}
	return false, f.start.Add(f.window).Sub(now)
}

type slidingWindow struct {
	mu     sync.Mutex
	events []time.Time
	limit  int
	window time.Duration
}

func (s *slidingWindow) Allow(_ string, now time.Time) (bool, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now.Add(-s.window)
	i := 0
	for i < len(s.events) && s.events[i].Before(cutoff) {
		i++
	}
	s.events = s.events[i:]
	if len(s.events) >= s.limit {
		return false, s.events[0].Add(s.window).Sub(now)
	}
	s.events = append(s.events, now)
	return true, 0
}

func makeLimiter(c RateLimitConfig) limiter {
	window := c.Window.Duration
	if window <= 0 {
		window = time.Minute
	}
	if c.Limit <= 0 {
		return nil
	}
	burst := c.Burst
	if burst <= 0 {
		burst = c.Limit
	}
	var factory func() limiter
	switch c.Pattern {
	case "fixed_window":
		factory = func() limiter { return &fixedWindow{limit: c.Limit, window: window} }
	case "sliding_window":
		factory = func() limiter { return &slidingWindow{limit: c.Limit, window: window} }
	default:
		factory = func() limiter { return &tokenBucket{rate: float64(c.Limit) / window.Seconds(), burst: float64(burst)} }
	}
	return newKeyedLimiter(factory)
}
