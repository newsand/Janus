package middleware_test

// T33 — characterization tests of the CURRENT JWTAuth and ServiceKeyAuth
// behavior (decision #8). They pin what the code does today, so the T37
// refactor can prove it changed nothing.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/middleware"
)

const (
	T33_JWT_SECRET    = "t33-test-jwt-secret-not-a-real-secret"
	T33_OTHER_SECRET  = "t33-some-other-secret-not-a-real-one"
	T33_SERVICE_KEY_A = "t33-service-key-a-placeholder"
	T33_SERVICE_KEY_B = "t33-service-key-b-placeholder"
)

// t33UseConfig sets the shared config singleton for one test and restores it.
func t33UseConfig(t *testing.T, change func(cfg *config.Config)) {
	t.Helper()
	cfg := config.Get()
	original := *cfg
	t.Cleanup(func() { *cfg = original })
	change(cfg)
}

type t33Seen struct {
	called         bool
	userID         any
	jti            any
	twoFAVerified  any
	serviceKeyAuth any
}

// t33Serve runs one request through the middleware in front of a dummy handler.
func t33Serve(t *testing.T, mw gin.HandlerFunc, authorization string) (*httptest.ResponseRecorder, *t33Seen) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	seen := &t33Seen{}
	r := gin.New()
	r.GET("/protected", mw, func(c *gin.Context) {
		seen.called = true
		seen.userID, _ = c.Get("user_id")
		seen.jti, _ = c.Get("jti")
		seen.twoFAVerified, _ = c.Get("2fa_verified")
		seen.serviceKeyAuth, _ = c.Get("service_key_auth")
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, seen
}

func t33ErrorBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not a JSON object: %q (%v)", w.Body.String(), err)
	}
	return body["error"]
}

func t33AssertUnauthorized(t *testing.T, w *httptest.ResponseRecorder, seen *t33Seen, wantError string) {
	t.Helper()
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if got := t33ErrorBody(t, w); got != wantError {
		t.Fatalf("error = %q, want %q", got, wantError)
	}
	if seen.called {
		t.Fatal("next handler was called after a 401")
	}
}

func t33SignToken(t *testing.T, method jwt.SigningMethod, secret string, claims middleware.Claims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func t33Claims(subject string, expiresIn time.Duration, twoFA bool) middleware.Claims {
	now := time.Now()
	return middleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ID:        "t33-jti-" + subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(expiresIn)),
		},
		TwoFAVerified: twoFA,
	}
}

func t33UseJWTSecret(t *testing.T) {
	t33UseConfig(t, func(cfg *config.Config) { cfg.JWTSecret = T33_JWT_SECRET })
}

// --- JWTAuth -----------------------------------------------------------------

func TestT33_JWTAuth_MissingHeader_Returns401(t *testing.T) {
	t33UseJWTSecret(t)
	w, seen := t33Serve(t, middleware.JWTAuth(), "")
	t33AssertUnauthorized(t, w, seen, "missing authorization header")
}

func TestT33_JWTAuth_NonBearerScheme_Returns401(t *testing.T) {
	t33UseJWTSecret(t)
	token := t33SignToken(t, jwt.SigningMethodHS256, T33_JWT_SECRET, t33Claims("user-1", time.Minute, false))
	w, seen := t33Serve(t, middleware.JWTAuth(), "Basic "+token)
	t33AssertUnauthorized(t, w, seen, "invalid authorization header")
}

func TestT33_JWTAuth_BearerWithoutToken_Returns401(t *testing.T) {
	t33UseJWTSecret(t)
	w, seen := t33Serve(t, middleware.JWTAuth(), "Bearer")
	t33AssertUnauthorized(t, w, seen, "invalid authorization header")
}

func TestT33_JWTAuth_TokenWithoutScheme_Returns401(t *testing.T) {
	t33UseJWTSecret(t)
	token := t33SignToken(t, jwt.SigningMethodHS256, T33_JWT_SECRET, t33Claims("user-1", time.Minute, false))
	w, seen := t33Serve(t, middleware.JWTAuth(), token)
	t33AssertUnauthorized(t, w, seen, "invalid authorization header")
}

func TestT33_JWTAuth_MalformedToken_Returns401(t *testing.T) {
	t33UseJWTSecret(t)
	w, seen := t33Serve(t, middleware.JWTAuth(), "Bearer not.a.jwt")
	t33AssertUnauthorized(t, w, seen, "invalid token")
}

func TestT33_JWTAuth_ExpiredToken_Returns401(t *testing.T) {
	t33UseJWTSecret(t)
	token := t33SignToken(t, jwt.SigningMethodHS256, T33_JWT_SECRET, t33Claims("user-1", -time.Minute, false))
	w, seen := t33Serve(t, middleware.JWTAuth(), "Bearer "+token)
	t33AssertUnauthorized(t, w, seen, "invalid token")
}

func TestT33_JWTAuth_SignedWithOtherSecret_Returns401(t *testing.T) {
	t33UseJWTSecret(t)
	token := t33SignToken(t, jwt.SigningMethodHS256, T33_OTHER_SECRET, t33Claims("user-1", time.Minute, false))
	w, seen := t33Serve(t, middleware.JWTAuth(), "Bearer "+token)
	t33AssertUnauthorized(t, w, seen, "invalid token")
}

func TestT33_JWTAuth_AlgNoneToken_Returns401(t *testing.T) {
	t33UseJWTSecret(t)
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, t33Claims("user-1", time.Minute, false)).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("build alg=none token: %v", err)
	}
	w, seen := t33Serve(t, middleware.JWTAuth(), "Bearer "+unsigned)
	t33AssertUnauthorized(t, w, seen, "invalid token")
}

func TestT33_JWTAuth_ValidToken_SetsSubJTIAnd2FAInContext(t *testing.T) {
	t33UseJWTSecret(t)
	token := t33SignToken(t, jwt.SigningMethodHS256, T33_JWT_SECRET, t33Claims("user-42", time.Minute, true))
	w, seen := t33Serve(t, middleware.JWTAuth(), "Bearer "+token)
	if w.Code != http.StatusOK || !seen.called {
		t.Fatalf("status = %d, called = %v; want 200 and next handler called", w.Code, seen.called)
	}
	if seen.userID != "user-42" {
		t.Fatalf("user_id = %v, want user-42", seen.userID)
	}
	if seen.jti != "t33-jti-user-42" {
		t.Fatalf("jti = %v, want t33-jti-user-42", seen.jti)
	}
	if seen.twoFAVerified != true {
		t.Fatalf("2fa_verified = %v, want true", seen.twoFAVerified)
	}
}

func TestT33_JWTAuth_ValidTokenWithout2FA_Sets2FAFalse(t *testing.T) {
	t33UseJWTSecret(t)
	token := t33SignToken(t, jwt.SigningMethodHS256, T33_JWT_SECRET, t33Claims("user-7", time.Minute, false))
	w, seen := t33Serve(t, middleware.JWTAuth(), "Bearer "+token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if seen.twoFAVerified != false {
		t.Fatalf("2fa_verified = %v, want false", seen.twoFAVerified)
	}
}

// Characterization: the scheme is matched case-insensitively.
func TestT33_JWTAuth_LowercaseBearerScheme_IsAccepted(t *testing.T) {
	t33UseJWTSecret(t)
	token := t33SignToken(t, jwt.SigningMethodHS256, T33_JWT_SECRET, t33Claims("user-1", time.Minute, false))
	w, seen := t33Serve(t, middleware.JWTAuth(), "bearer "+token)
	if w.Code != http.StatusOK || seen.userID != "user-1" {
		t.Fatalf("status = %d, user_id = %v; want 200 and user-1", w.Code, seen.userID)
	}
}

// Characterization: any HMAC algorithm with the shared secret is accepted, not only HS256.
func TestT33_JWTAuth_HS512WithSameSecret_IsAccepted(t *testing.T) {
	t33UseJWTSecret(t)
	token := t33SignToken(t, jwt.SigningMethodHS512, T33_JWT_SECRET, t33Claims("user-512", time.Minute, false))
	w, seen := t33Serve(t, middleware.JWTAuth(), "Bearer "+token)
	if w.Code != http.StatusOK || seen.userID != "user-512" {
		t.Fatalf("status = %d, user_id = %v; want 200 and user-512", w.Code, seen.userID)
	}
}

// Characterization: a token without exp is accepted (no exp requirement today).
func TestT33_JWTAuth_TokenWithoutExp_IsAccepted(t *testing.T) {
	t33UseJWTSecret(t)
	claims := middleware.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-noexp"}}
	token := t33SignToken(t, jwt.SigningMethodHS256, T33_JWT_SECRET, claims)
	w, seen := t33Serve(t, middleware.JWTAuth(), "Bearer "+token)
	if w.Code != http.StatusOK || seen.userID != "user-noexp" {
		t.Fatalf("status = %d, user_id = %v; want 200 and user-noexp", w.Code, seen.userID)
	}
}

// --- ServiceKeyAuth ----------------------------------------------------------

func t33UseServiceKeys(t *testing.T, keys ...string) {
	t33UseConfig(t, func(cfg *config.Config) { cfg.ServiceKeys = keys })
}

func TestT33_ServiceKeyAuth_MissingHeader_Returns401(t *testing.T) {
	t33UseServiceKeys(t, T33_SERVICE_KEY_A)
	w, seen := t33Serve(t, middleware.ServiceKeyAuth(), "")
	t33AssertUnauthorized(t, w, seen, "missing authorization header")
}

func TestT33_ServiceKeyAuth_NonBearerScheme_Returns401(t *testing.T) {
	t33UseServiceKeys(t, T33_SERVICE_KEY_A)
	w, seen := t33Serve(t, middleware.ServiceKeyAuth(), "Basic "+T33_SERVICE_KEY_A)
	t33AssertUnauthorized(t, w, seen, "invalid authorization header")
}

func TestT33_ServiceKeyAuth_KeyWithoutScheme_Returns401(t *testing.T) {
	t33UseServiceKeys(t, T33_SERVICE_KEY_A)
	w, seen := t33Serve(t, middleware.ServiceKeyAuth(), T33_SERVICE_KEY_A)
	t33AssertUnauthorized(t, w, seen, "invalid authorization header")
}

func TestT33_ServiceKeyAuth_WrongKey_Returns401(t *testing.T) {
	t33UseServiceKeys(t, T33_SERVICE_KEY_A)
	w, seen := t33Serve(t, middleware.ServiceKeyAuth(), "Bearer t33-wrong-key")
	t33AssertUnauthorized(t, w, seen, "invalid service key")
}

func TestT33_ServiceKeyAuth_NoKeysConfigured_RejectsAnyKey(t *testing.T) {
	t33UseServiceKeys(t)
	w, seen := t33Serve(t, middleware.ServiceKeyAuth(), "Bearer "+T33_SERVICE_KEY_A)
	t33AssertUnauthorized(t, w, seen, "invalid service key")
}

func TestT33_ServiceKeyAuth_ValidKey_PassesAndMarksContext(t *testing.T) {
	t33UseServiceKeys(t, T33_SERVICE_KEY_A, T33_SERVICE_KEY_B)
	for _, key := range []string{T33_SERVICE_KEY_A, T33_SERVICE_KEY_B} {
		w, seen := t33Serve(t, middleware.ServiceKeyAuth(), "Bearer "+key)
		if w.Code != http.StatusOK || !seen.called {
			t.Fatalf("key %q: status = %d, called = %v; want 200 and next handler called", key, w.Code, seen.called)
		}
		if seen.serviceKeyAuth != true {
			t.Fatalf("key %q: service_key_auth = %v, want true", key, seen.serviceKeyAuth)
		}
	}
}

func TestT33_ServiceKeyAuth_LowercaseBearerScheme_IsAccepted(t *testing.T) {
	t33UseServiceKeys(t, T33_SERVICE_KEY_A)
	w, seen := t33Serve(t, middleware.ServiceKeyAuth(), "bearer "+T33_SERVICE_KEY_A)
	if w.Code != http.StatusOK || !seen.called {
		t.Fatalf("status = %d, called = %v; want 200 and next handler called", w.Code, seen.called)
	}
}
