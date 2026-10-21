package gateway

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

// limiter holds the state of one key. Implementations are not safe for
// concurrent use; keyedLimiter serializes every call under its own mutex.
type limiter interface {
	Allow(now time.Time) (bool, time.Duration)
}
type limiterEntry struct {
	l    limiter
	seen time.Time
}
type keyedLimiter struct {
	newLimiter func() limiter
	// idleTTL is how long a key must stay idle before its state equals a fresh
	// limiter, so dropping it cannot let a client through earlier.
	idleTTL   time.Duration
	mu        sync.Mutex
	entries   map[string]*limiterEntry
	lastSweep time.Time
}

func newKeyedLimiter(newLimiter func() limiter, idleTTL time.Duration) *keyedLimiter {
	return &keyedLimiter{newLimiter: newLimiter, idleTTL: idleTTL, entries: map[string]*limiterEntry{}}
}
func (k *keyedLimiter) Allow(key string, now time.Time) (bool, time.Duration) {
	k.mu.Lock()
	defer k.mu.Unlock()
	// ponytail: O(keys) sweep under the global lock once per idleTTL; shard the
	// map if key cardinality ever makes that pause visible.
	if now.Sub(k.lastSweep) >= k.idleTTL {
		for idleKey, e := range k.entries {
			if now.Sub(e.seen) >= k.idleTTL {
				delete(k.entries, idleKey)
			}
		}
		k.lastSweep = now
	}
	e, ok := k.entries[key]
	if !ok {
		e = &limiterEntry{l: k.newLimiter()}
		k.entries[key] = e
	}
	e.seen = now
	return e.l.Allow(now)
}

type tokenBucket struct {
	tokens float64
	last   time.Time
	rate   float64
	burst  float64
}

func (t *tokenBucket) Allow(now time.Time) (bool, time.Duration) {
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
	start        time.Time
	count, limit int
	window       time.Duration
}

func (f *fixedWindow) Allow(now time.Time) (bool, time.Duration) {
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
	events []time.Time
	limit  int
	window time.Duration
}

func (s *slidingWindow) Allow(now time.Time) (bool, time.Duration) {
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

func makeLimiter(c RateLimitConfig) (*keyedLimiter, error) {
	window := c.Window.Duration
	if window <= 0 {
		window = time.Minute
	}
	if c.Limit <= 0 {
		return nil, errors.New("limit must be positive")
	}
	burst := c.Burst
	if burst <= 0 {
		burst = c.Limit
	}
	switch c.Pattern {
	case "fixed_window":
		return newKeyedLimiter(func() limiter { return &fixedWindow{limit: c.Limit, window: window} }, window), nil
	case "sliding_window":
		return newKeyedLimiter(func() limiter { return &slidingWindow{limit: c.Limit, window: window} }, window), nil
	case "", "token_bucket":
		rate := float64(c.Limit) / window.Seconds()
		// An empty bucket is full again after burst/rate seconds.
		refill := time.Duration(float64(burst) / rate * float64(time.Second))
		return newKeyedLimiter(func() limiter { return &tokenBucket{rate: rate, burst: float64(burst)} }, max(window, refill)), nil
	default:
		return nil, fmt.Errorf("unknown pattern %q (want token_bucket, fixed_window or sliding_window)", c.Pattern)
	}
}
