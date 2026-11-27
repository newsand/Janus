package middleware_test

// T33 — characterization tests of the CURRENT RateLimit and lockout behavior
// (decision #8). The in-memory state is package-global, so every test uses its
// own unique path / identifier instead of resetting it.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/middleware"
)

func t33UseRateLimit(t *testing.T, requests int, window time.Duration) {
	t33UseConfig(t, func(cfg *config.Config) {
		cfg.RateLimitRequests = requests
		cfg.RateLimitWindow = window
	})
}

func t33UseLockout(t *testing.T, threshold int, duration time.Duration) {
	t33UseConfig(t, func(cfg *config.Config) {
		cfg.LockoutThreshold = threshold
		cfg.LockoutDuration = duration
	})
}

// t33RateLimitedEngine builds an engine with RateLimit on every given path.
func t33RateLimitedEngine(paths ...string) (*gin.Engine, *int) {
	gin.SetMode(gin.TestMode)
	calls := 0
	r := gin.New()
	limiter := middleware.RateLimit()
	for _, p := range paths {
		r.GET(p, limiter, func(c *gin.Context) {
			calls++
			c.Status(http.StatusOK)
		})
	}
	return r, &calls
}

func t33Get(r *gin.Engine, path, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func t33AssertStatus(t *testing.T, w *httptest.ResponseRecorder, want int, step string) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("%s: status = %d, want %d", step, w.Code, want)
	}
}

// --- RateLimit ---------------------------------------------------------------

func TestT33_RateLimit_OverConfiguredLimit_Returns429(t *testing.T) {
	t33UseRateLimit(t, 2, time.Minute)
	path := "/t33/ratelimit/over-limit"
	r, calls := t33RateLimitedEngine(path)

	t33AssertStatus(t, t33Get(r, path, "10.33.0.1:1234"), http.StatusOK, "request 1")
	t33AssertStatus(t, t33Get(r, path, "10.33.0.1:1234"), http.StatusOK, "request 2")
	w := t33Get(r, path, "10.33.0.1:1234")
	t33AssertStatus(t, w, http.StatusTooManyRequests, "request 3")

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["error"] != "rate limit exceeded" {
		t.Fatalf("body = %q, want {\"error\":\"rate limit exceeded\"}", w.Body.String())
	}
	if *calls != 2 {
		t.Fatalf("handler calls = %d, want 2 (blocked request must not reach the handler)", *calls)
	}
}

func TestT33_RateLimit_AtLimit_IsAllowed(t *testing.T) {
	t33UseRateLimit(t, 3, time.Minute)
	path := "/t33/ratelimit/at-limit"
	r, _ := t33RateLimitedEngine(path)
	for i := 1; i <= 3; i++ {
		t33AssertStatus(t, t33Get(r, path, "10.33.0.2:1234"), http.StatusOK, "request within limit")
	}
}

func TestT33_RateLimit_CountsPerPath(t *testing.T) {
	t33UseRateLimit(t, 1, time.Minute)
	pathA, pathB := "/t33/ratelimit/per-path-a", "/t33/ratelimit/per-path-b"
	r, _ := t33RateLimitedEngine(pathA, pathB)

	t33AssertStatus(t, t33Get(r, pathA, "10.33.0.3:1234"), http.StatusOK, "path A request 1")
	t33AssertStatus(t, t33Get(r, pathA, "10.33.0.3:1234"), http.StatusTooManyRequests, "path A request 2")
	t33AssertStatus(t, t33Get(r, pathB, "10.33.0.3:1234"), http.StatusOK, "path B request 1")
}

func TestT33_RateLimit_CountsPerClientIP(t *testing.T) {
	t33UseRateLimit(t, 1, time.Minute)
	path := "/t33/ratelimit/per-ip"
	r, _ := t33RateLimitedEngine(path)

	t33AssertStatus(t, t33Get(r, path, "10.33.0.4:1234"), http.StatusOK, "ip 4 request 1")
	t33AssertStatus(t, t33Get(r, path, "10.33.0.4:1234"), http.StatusTooManyRequests, "ip 4 request 2")
	t33AssertStatus(t, t33Get(r, path, "10.33.0.5:1234"), http.StatusOK, "ip 5 request 1")
}

func TestT33_RateLimit_AfterWindowEnds_AllowsAgain(t *testing.T) {
	t33UseRateLimit(t, 1, 300*time.Millisecond)
	path := "/t33/ratelimit/window-reset"
	r, _ := t33RateLimitedEngine(path)

	t33AssertStatus(t, t33Get(r, path, "10.33.0.6:1234"), http.StatusOK, "request 1")
	t33AssertStatus(t, t33Get(r, path, "10.33.0.6:1234"), http.StatusTooManyRequests, "request 2")
	time.Sleep(600 * time.Millisecond)
	t33AssertStatus(t, t33Get(r, path, "10.33.0.6:1234"), http.StatusOK, "request after window")
}

// --- Lockout -----------------------------------------------------------------

func TestT33_Lockout_UnknownIdentifier_IsNotLocked(t *testing.T) {
	t33UseLockout(t, 3, time.Minute)
	if middleware.CheckLockout(context.Background(), "t33-lockout-unknown") {
		t.Fatal("CheckLockout = true for an identifier with no failures")
	}
}

func TestT33_Lockout_ReachingThreshold_Locks(t *testing.T) {
	t33UseLockout(t, 3, time.Minute)
	id := "t33-lockout-threshold"

	for i := 1; i <= 2; i++ {
		if middleware.RecordFailedAttempt(id) {
			t.Fatalf("RecordFailedAttempt #%d = true, want false (below threshold)", i)
		}
		if middleware.CheckLockout(context.Background(), id) {
			t.Fatalf("CheckLockout after %d failures = true, want false", i)
		}
	}
	if !middleware.RecordFailedAttempt(id) {
		t.Fatal("RecordFailedAttempt #3 = false, want true (threshold reached)")
	}
	if !middleware.CheckLockout(context.Background(), id) {
		t.Fatal("CheckLockout after threshold = false, want true")
	}
}

func TestT33_Lockout_FailureWhileLocked_StaysLocked(t *testing.T) {
	t33UseLockout(t, 2, time.Minute)
	id := "t33-lockout-while-locked"
	middleware.RecordFailedAttempt(id)
	middleware.RecordFailedAttempt(id)

	if !middleware.RecordFailedAttempt(id) {
		t.Fatal("RecordFailedAttempt while locked = false, want true")
	}
	if !middleware.CheckLockout(context.Background(), id) {
		t.Fatal("CheckLockout while locked = false, want true")
	}
}

func TestT33_Lockout_AfterDuration_UnlocksAndRestartsCount(t *testing.T) {
	t33UseLockout(t, 2, 30*time.Millisecond)
	id := "t33-lockout-expiry-check"
	middleware.RecordFailedAttempt(id)
	if !middleware.RecordFailedAttempt(id) {
		t.Fatal("RecordFailedAttempt #2 = false, want true")
	}
	time.Sleep(60 * time.Millisecond)

	if middleware.CheckLockout(context.Background(), id) {
		t.Fatal("CheckLockout after duration = true, want false")
	}
	if middleware.RecordFailedAttempt(id) {
		t.Fatal("first failure after expiry = true, want false (count restarted)")
	}
}

func TestT33_Lockout_ExpiredWithoutCheck_NextFailureRestartsCount(t *testing.T) {
	t33UseLockout(t, 2, 30*time.Millisecond)
	id := "t33-lockout-expiry-record"
	middleware.RecordFailedAttempt(id)
	middleware.RecordFailedAttempt(id)
	time.Sleep(60 * time.Millisecond)

	if middleware.RecordFailedAttempt(id) {
		t.Fatal("first failure after expiry = true, want false (count reset to 1)")
	}
	if !middleware.RecordFailedAttempt(id) {
		t.Fatal("second failure after expiry = false, want true (threshold reached again)")
	}
}

func TestT33_Lockout_ClearLockout_UnlocksAndRestartsCount(t *testing.T) {
	t33UseLockout(t, 2, time.Minute)
	id := "t33-lockout-clear"
	middleware.RecordFailedAttempt(id)
	middleware.RecordFailedAttempt(id)
	if !middleware.CheckLockout(context.Background(), id) {
		t.Fatal("precondition: CheckLockout = false, want true")
	}

	middleware.ClearLockout(id)

	if middleware.CheckLockout(context.Background(), id) {
		t.Fatal("CheckLockout after ClearLockout = true, want false")
	}
	if middleware.RecordFailedAttempt(id) {
		t.Fatal("first failure after ClearLockout = true, want false")
	}
}

func TestT33_Lockout_ClearUnknownIdentifier_IsNoop(t *testing.T) {
	t33UseLockout(t, 2, time.Minute)
	middleware.ClearLockout("t33-lockout-never-seen")
	if middleware.CheckLockout(context.Background(), "t33-lockout-never-seen") {
		t.Fatal("CheckLockout = true after clearing an unknown identifier")
	}
}

// Characterization: with threshold 1 the identifier never locks — the first
// failure only creates the entry and every later failure resets the count to 1.
func TestT33_Lockout_ThresholdOne_NeverLocks(t *testing.T) {
	t33UseLockout(t, 1, time.Minute)
	id := "t33-lockout-threshold-one"
	for i := 1; i <= 3; i++ {
		if middleware.RecordFailedAttempt(id) {
			t.Fatalf("RecordFailedAttempt #%d = true, want false", i)
		}
		if middleware.CheckLockout(context.Background(), id) {
			t.Fatalf("CheckLockout after %d failures = true, want false", i)
		}
	}
}
