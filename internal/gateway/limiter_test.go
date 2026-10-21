package gateway

import (
	"testing"
	"time"
)

func TestLimiters(t *testing.T) {
	start := time.Unix(0, 0)
	at := func(offset time.Duration) time.Time { return start.Add(offset) }
	for _, tc := range []struct {
		name    string
		config  RateLimitConfig
		calls   []time.Duration
		allowed []bool
	}{
		{"token bucket spends burst then refills at limit/window", RateLimitConfig{Limit: 60, Window: Duration{time.Minute}, Burst: 2},
			[]time.Duration{0, 0, 0, time.Second}, []bool{true, true, false, true}},
		{"fixed window resets at the window boundary", RateLimitConfig{Pattern: "fixed_window", Limit: 2, Window: Duration{time.Minute}},
			[]time.Duration{0, 30 * time.Second, 59 * time.Second, time.Minute}, []bool{true, true, false, true}},
		{"sliding window frees a slot when the oldest event ages out", RateLimitConfig{Pattern: "sliding_window", Limit: 2, Window: Duration{time.Minute}},
			[]time.Duration{0, 30 * time.Second, 59 * time.Second, 61 * time.Second, 62 * time.Second}, []bool{true, true, false, true, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := makeLimiter(tc.config)
			for i, offset := range tc.calls {
				if got, _ := l.Allow("k", at(offset)); got != tc.allowed[i] {
					t.Fatalf("call %d at %v: got %v, want %v", i, offset, got, tc.allowed[i])
				}
			}
		})
	}
}

func TestKeyedLimiterEvictsIdleKeys(t *testing.T) {
	l := makeLimiter(RateLimitConfig{Pattern: "fixed_window", Limit: 1, Window: Duration{time.Minute}})
	now := time.Unix(0, 0)
	for _, key := range []string{"a", "b", "c"} {
		l.Allow(key, now)
	}
	l.Allow("d", now.Add(time.Minute))
	if len(l.entries) != 1 {
		t.Fatalf("got %d entries after the idle TTL, want only the active key", len(l.entries))
	}
}
