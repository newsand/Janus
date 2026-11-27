package middleware

import (
	"strconv"
	"testing"
	"time"
)

// A shard held at sweepAfter entries must not rescan its map on every call:
// the sweep runs at most once per sweepInterval.
func TestSweepRates_RunsAtMostOncePerInterval(t *testing.T) {
	s := &limiterShard{rates: map[string]*rateLimitEntry{}}
	now := time.Now()
	fill := func() {
		for i := 0; i < sweepAfter; i++ {
			s.rates["k"+strconv.Itoa(i)] = &rateLimitEntry{count: 1, windowEnd: now.Add(-time.Second)}
		}
	}

	fill()
	s.sweepRates(now)
	if len(s.rates) != 0 {
		t.Fatalf("first sweep left %d ended windows, want 0", len(s.rates))
	}

	fill()
	s.sweepRates(now.Add(sweepInterval / 2))
	if len(s.rates) != sweepAfter {
		t.Fatalf("sweep inside interval removed entries: len = %d, want %d", len(s.rates), sweepAfter)
	}

	s.sweepRates(now.Add(sweepInterval))
	if len(s.rates) != 0 {
		t.Fatalf("sweep after interval left %d ended windows, want 0", len(s.rates))
	}
}

func TestSweepLockouts_KeepsPartialCountsAndIsThrottled(t *testing.T) {
	s := &limiterShard{lockouts: map[string]*lockoutEntry{}}
	now := time.Now()
	const threshold = 3
	for i := 0; i < sweepAfter; i++ {
		s.lockouts["p"+strconv.Itoa(i)] = &lockoutEntry{failCount: 1}
	}
	s.lockouts["expired"] = &lockoutEntry{failCount: threshold, lockedUntil: now.Add(-time.Second)}

	s.sweepLockouts(now, threshold)
	if _, ok := s.lockouts["expired"]; ok {
		t.Fatal("expired lock was not swept")
	}
	if len(s.lockouts) != sweepAfter {
		t.Fatalf("partial counts swept: len = %d, want %d", len(s.lockouts), sweepAfter)
	}

	s.lockouts["expired2"] = &lockoutEntry{failCount: threshold, lockedUntil: now.Add(-time.Second)}
	s.sweepLockouts(now.Add(time.Second), threshold)
	if _, ok := s.lockouts["expired2"]; !ok {
		t.Fatal("sweep ran again inside the interval")
	}
	s.sweepLockouts(now.Add(sweepInterval), threshold)
	if _, ok := s.lockouts["expired2"]; ok {
		t.Fatal("expired lock not swept after the interval")
	}
}
