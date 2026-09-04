package auth

import (
	"testing"
	"time"
)

func TestIPLimiter_AllowsUntilThreshold(t *testing.T) {
	l := newIPLimiter()
	const ip = "203.0.113.1"

	for i := 0; i < ipRateLimitMax; i++ {
		if !l.allow(ip) {
			t.Fatalf("allow() = false on attempt %d, want true (limit is %d)", i+1, ipRateLimitMax)
		}
		l.recordFailure(ip)
	}
	if l.allow(ip) {
		t.Fatalf("allow() = true after %d recorded failures, want false", ipRateLimitMax)
	}
}

func TestIPLimiter_TracksIPsIndependently(t *testing.T) {
	l := newIPLimiter()
	for i := 0; i < ipRateLimitMax; i++ {
		l.recordFailure("203.0.113.1")
	}
	if l.allow("203.0.113.1") {
		t.Fatal("allow() = true for the throttled IP, want false")
	}
	if !l.allow("203.0.113.2") {
		t.Fatal("allow() = false for an unrelated IP, want true")
	}
}

func TestIPLimiter_PrunesEntriesOutsideWindow(t *testing.T) {
	l := newIPLimiter()
	const ip = "203.0.113.1"

	// Seed timestamps older than the 15-minute window directly, rather
	// than sleeping in a test, to exercise prune()'s cutoff logic.
	l.mu.Lock()
	old := time.Now().Add(-ipRateLimitWindow - time.Minute)
	hits := make([]time.Time, ipRateLimitMax)
	for i := range hits {
		hits[i] = old
	}
	l.hits[ip] = hits
	l.mu.Unlock()

	if !l.allow(ip) {
		t.Fatal("allow() = false for an IP whose only recorded failures are outside the window, want true")
	}

	l.mu.Lock()
	_, stillTracked := l.hits[ip]
	l.mu.Unlock()
	if stillTracked {
		t.Fatal("expired IP entry should have been pruned from the map, not just filtered at read time")
	}
}
