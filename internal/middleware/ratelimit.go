package middleware

import (
	"context"
	"hash/fnv"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/config"
)

type rateLimitEntry struct {
	count     int
	windowEnd time.Time
}

type lockoutEntry struct {
	failCount   int
	lockedUntil time.Time
}

const (
	limiterShards = 32
	sweepAfter    = 1024
)

type limiterShard struct {
	mu       sync.Mutex
	rates    map[string]*rateLimitEntry
	lockouts map[string]*lockoutEntry
}

var shards [limiterShards]limiterShard

func init() {
	for i := range shards {
		shards[i].rates = make(map[string]*rateLimitEntry)
		shards[i].lockouts = make(map[string]*lockoutEntry)
	}
}

func shardOf(key string) *limiterShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &shards[h.Sum32()%limiterShards]
}

func RateLimit() gin.HandlerFunc {
	cfg := config.Get()
	return func(c *gin.Context) {
		key := c.ClientIP() + ":" + c.Request.URL.Path
		now := time.Now()
		s := shardOf(key)

		s.mu.Lock()
		s.sweepRates(now)
		entry, exists := s.rates[key]

		if !exists || now.After(entry.windowEnd) {
			s.rates[key] = &rateLimitEntry{
				count:     1,
				windowEnd: now.Add(cfg.RateLimitWindow),
			}
			s.mu.Unlock()
			c.Next()
			return
		}

		entry.count++
		if entry.count > cfg.RateLimitRequests {
			s.mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}
		s.mu.Unlock()
		c.Next()
	}
}

func (s *limiterShard) sweepRates(now time.Time) {
	if len(s.rates) < sweepAfter {
		return
	}
	for k, e := range s.rates {
		if now.After(e.windowEnd) {
			delete(s.rates, k)
		}
	}
}

func CheckLockout(ctx context.Context, identifier string) bool {
	cfg := config.Get()
	now := time.Now()
	s := shardOf(identifier)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLockouts(now, cfg.LockoutThreshold)

	entry, exists := s.lockouts[identifier]
	if !exists {
		return false
	}

	if now.Before(entry.lockedUntil) {
		return true
	}

	if entry.failCount >= cfg.LockoutThreshold && now.After(entry.lockedUntil) {
		delete(s.lockouts, identifier)
	}

	return false
}

func RecordFailedAttempt(identifier string) bool {
	cfg := config.Get()
	s := shardOf(identifier)

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.lockouts[identifier]
	now := time.Now()

	if !exists {
		s.lockouts[identifier] = &lockoutEntry{
			failCount:   1,
			lockedUntil: time.Time{},
		}
		return false
	}

	if now.After(entry.lockedUntil) && entry.failCount >= cfg.LockoutThreshold {
		entry.failCount = 1
		entry.lockedUntil = time.Time{}
		return false
	}

	entry.failCount++
	if entry.failCount >= cfg.LockoutThreshold {
		entry.lockedUntil = now.Add(cfg.LockoutDuration)
		return true
	}

	return false
}

func ClearLockout(identifier string) {
	s := shardOf(identifier)
	s.mu.Lock()
	delete(s.lockouts, identifier)
	s.mu.Unlock()
}

func (s *limiterShard) sweepLockouts(now time.Time, threshold int) {
	if len(s.lockouts) < sweepAfter {
		return
	}
	for k, e := range s.lockouts {
		if e.failCount >= threshold && !e.lockedUntil.IsZero() && now.After(e.lockedUntil) {
			delete(s.lockouts, k)
		}
	}
}
