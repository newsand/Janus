package password

// T31 — characterization tests of the CURRENT /v1/password/forgot and
// /v1/password/reset behavior (decision #8). They pin what the code does today,
// so the T36 refactor can prove it changed nothing.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	FORGOT_PATH         = "/v1/password/forgot"
	RESET_PATH          = "/v1/password/reset"
	GENERIC_FORGOT_BODY = `{"message":"if the email exists, a recovery link will be sent"}`
	RESET_OK_BODY       = `{"message":"password reset successful"}`
	INVALID_TOKEN_BODY  = `{"error":"invalid or expired token"}`
	OLD_PASSWORD        = "oldpassword123"
	NEW_PASSWORD        = "newpassword123"
)

// clientCounter gives every request its own client IP, so the package-global
// rate limiter (5 req/min per IP+path) never interferes with unrelated cases.
var clientCounter atomic.Int64

func nextClientAddr() string {
	n := clientCounter.Add(1)
	return fmt.Sprintf("10.31.%d.%d:1234", (n/250)%250, n%250+1)
}

func setupPasswordTestDB(t *testing.T) {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	if err := testDB.AutoMigrate(&models.User{}, &models.RecoverToken{}, &models.RefreshToken{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	db.SetTestDB(testDB)
}

func setupPasswordTestRouter() *gin.Engine {
	config.Load()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r.Group("/v1"))
	return r
}

func setupPasswordTest(t *testing.T) *gin.Engine {
	t.Helper()
	setupPasswordTestDB(t)
	return setupPasswordTestRouter()
}

func createPasswordTestUser(t *testing.T, email string, disabled bool) *models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(OLD_PASSWORD), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	passwordHash := string(hash)
	user := &models.User{Email: email, PasswordHash: &passwordHash, Nome: "Test User"}
	if disabled {
		now := time.Now()
		user.DisabledAt = &now
	}
	if err := db.DB().Create(user).Error; err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	return user
}

func createRecoverToken(t *testing.T, userID, plain string, expiresAt time.Time, usedAt *time.Time) *models.RecoverToken {
	t.Helper()
	rt := &models.RecoverToken{UserID: userID, TokenHash: models.HashToken(plain), ExpiresAt: expiresAt, UsedAt: usedAt}
	if err := db.DB().Create(rt).Error; err != nil {
		t.Fatalf("failed to create recover token: %v", err)
	}
	return rt
}

func createActiveRefreshToken(t *testing.T, userID, plain string) {
	t.Helper()
	rt := &models.RefreshToken{UserID: userID, TokenHash: models.HashToken(plain), FamilyID: "11111111-1111-4111-8111-111111111111", ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.DB().Create(rt).Error; err != nil {
		t.Fatalf("failed to create refresh token: %v", err)
	}
}

func postRaw(router *gin.Engine, path, body, clientAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = clientAddr
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func postJSON(t *testing.T, router *gin.Engine, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	return postRaw(router, path, string(body), nextClientAddr())
}

func countRecoverTokens(t *testing.T, userID string, onlyActive bool) int64 {
	t.Helper()
	q := db.DB().Model(&models.RecoverToken{})
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	if onlyActive {
		q = q.Where("used_at IS NULL")
	}
	var n int64
	q.Count(&n)
	return n
}

func countActiveRefreshTokens(t *testing.T, userID string) int64 {
	t.Helper()
	var n int64
	db.DB().Model(&models.RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", userID).Count(&n)
	return n
}

func passwordMatches(t *testing.T, userID, password string) bool {
	t.Helper()
	var user models.User
	if err := db.DB().First(&user, "id = ?", userID).Error; err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	return user.PasswordHash != nil && bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(password)) == nil
}

func assertResponse(t *testing.T, w *httptest.ResponseRecorder, wantCode int, wantBody string) {
	t.Helper()
	if w.Code != wantCode {
		t.Fatalf("expected status %d, got %d: %s", wantCode, w.Code, w.Body.String())
	}
	if w.Body.String() != wantBody {
		t.Fatalf("expected body %s, got %s", wantBody, w.Body.String())
	}
}

// --- Forgot ---

func TestT31ForgotExistingEmailCreatesOneActiveRecoverToken(t *testing.T) {
	router := setupPasswordTest(t)
	user := createPasswordTestUser(t, "ana@example.com", false)

	before := time.Now()
	w := postJSON(t, router, FORGOT_PATH, map[string]string{"email": "ana@example.com"})

	assertResponse(t, w, http.StatusOK, GENERIC_FORGOT_BODY)
	var tokens []models.RecoverToken
	db.DB().Where("user_id = ?", user.ID).Find(&tokens)
	if len(tokens) != 1 {
		t.Fatalf("expected 1 recover token, got %d", len(tokens))
	}
	rt := tokens[0]
	if rt.UsedAt != nil {
		t.Fatalf("expected new recover token unused, got used_at=%v", rt.UsedAt)
	}
	if len(rt.TokenHash) != 64 {
		t.Fatalf("expected sha256 hex token_hash (64 chars), got %q", rt.TokenHash)
	}
	ttl := config.Get().RecoverTTL
	if rt.ExpiresAt.Before(before.Add(ttl-time.Minute)) || rt.ExpiresAt.After(time.Now().Add(ttl+time.Minute)) {
		t.Fatalf("expected expires_at ≈ now+%v, got %v", ttl, rt.ExpiresAt)
	}
}

func TestT31ForgotUnknownEmailSameGenericResponseNoToken(t *testing.T) {
	router := setupPasswordTest(t)
	createPasswordTestUser(t, "ana@example.com", false)

	existing := postJSON(t, router, FORGOT_PATH, map[string]string{"email": "ana@example.com"})
	unknown := postJSON(t, router, FORGOT_PATH, map[string]string{"email": "ghost@example.com"})

	assertResponse(t, unknown, http.StatusOK, GENERIC_FORGOT_BODY)
	if existing.Code != unknown.Code || existing.Body.String() != unknown.Body.String() {
		t.Fatalf("enumeration: existing=%d %s vs unknown=%d %s", existing.Code, existing.Body.String(), unknown.Code, unknown.Body.String())
	}
	if n := countRecoverTokens(t, "", false); n != 1 {
		t.Fatalf("expected only the existing user's token (1), got %d", n)
	}
}

func TestT31ForgotDisabledUserGenericResponseNoToken(t *testing.T) {
	router := setupPasswordTest(t)
	user := createPasswordTestUser(t, "off@example.com", true)

	w := postJSON(t, router, FORGOT_PATH, map[string]string{"email": "off@example.com"})

	assertResponse(t, w, http.StatusOK, GENERIC_FORGOT_BODY)
	if n := countRecoverTokens(t, user.ID, false); n != 0 {
		t.Fatalf("expected no recover token for disabled user, got %d", n)
	}
}

// Characterization: invalid input on /forgot does NOT return 400 today — it
// answers the same generic 200 (no enumeration via validation errors).
func TestT31ForgotInvalidInputGenericResponseNoToken(t *testing.T) {
	cases := map[string]string{
		"invalid email": `{"email":"not-an-email"}`,
		"missing email": `{}`,
		"empty email":   `{"email":""}`,
		"malformed":     `{"email":`,
		"sql-ish email": `{"email":"' OR 1=1 --"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			router := setupPasswordTest(t)
			createPasswordTestUser(t, "ana@example.com", false)

			w := postRaw(router, FORGOT_PATH, body, nextClientAddr())

			assertResponse(t, w, http.StatusOK, GENERIC_FORGOT_BODY)
			if n := countRecoverTokens(t, "", false); n != 0 {
				t.Fatalf("expected no recover token, got %d", n)
			}
		})
	}
}

func TestT31ForgotTwiceInvalidatesPreviousToken(t *testing.T) {
	router := setupPasswordTest(t)
	user := createPasswordTestUser(t, "ana@example.com", false)
	createRecoverToken(t, user.ID, "older-plain-token", time.Now().Add(time.Hour), nil)

	w := postJSON(t, router, FORGOT_PATH, map[string]string{"email": "ana@example.com"})

	assertResponse(t, w, http.StatusOK, GENERIC_FORGOT_BODY)
	if n := countRecoverTokens(t, user.ID, false); n != 2 {
		t.Fatalf("expected 2 recover tokens (old + new), got %d", n)
	}
	if n := countRecoverTokens(t, user.ID, true); n != 1 {
		t.Fatalf("expected exactly 1 active recover token, got %d", n)
	}
	var old models.RecoverToken
	db.DB().Where("token_hash = ?", models.HashToken("older-plain-token")).First(&old)
	if old.UsedAt == nil {
		t.Fatalf("expected previous recover token marked used")
	}
}

func TestT31ForgotRateLimitedAfterFiveRequestsPerClient(t *testing.T) {
	router := setupPasswordTest(t)
	addr := nextClientAddr()
	body := `{"email":"ghost@example.com"}`

	for i := 1; i <= config.Get().RateLimitRequests; i++ {
		if w := postRaw(router, FORGOT_PATH, body, addr); w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, w.Code)
		}
	}
	w := postRaw(router, FORGOT_PATH, body, addr)
	assertResponse(t, w, http.StatusTooManyRequests, `{"error":"rate limit exceeded"}`)
}

// --- Reset ---

func TestT31ResetValidTokenChangesPasswordAndRevokesRefreshTokens(t *testing.T) {
	router := setupPasswordTest(t)
	user := createPasswordTestUser(t, "ana@example.com", false)
	other := createPasswordTestUser(t, "bruno@example.com", false)
	rt := createRecoverToken(t, user.ID, "valid-plain-token", time.Now().Add(time.Hour), nil)
	createActiveRefreshToken(t, user.ID, "refresh-a1")
	createActiveRefreshToken(t, user.ID, "refresh-a2")
	createActiveRefreshToken(t, other.ID, "refresh-b1")

	w := postJSON(t, router, RESET_PATH, map[string]string{"token": "valid-plain-token", "new_password": NEW_PASSWORD})

	assertResponse(t, w, http.StatusOK, RESET_OK_BODY)
	if !passwordMatches(t, user.ID, NEW_PASSWORD) {
		t.Fatalf("expected new password to match stored hash")
	}
	if passwordMatches(t, user.ID, OLD_PASSWORD) {
		t.Fatalf("expected old password to no longer match")
	}
	var reloaded models.RecoverToken
	db.DB().First(&reloaded, "id = ?", rt.ID)
	if reloaded.UsedAt == nil {
		t.Fatalf("expected recover token marked used")
	}
	if n := countActiveRefreshTokens(t, user.ID); n != 0 {
		t.Fatalf("expected user's refresh tokens revoked, %d still active", n)
	}
	if n := countActiveRefreshTokens(t, other.ID); n != 1 {
		t.Fatalf("expected other user's refresh token untouched, got %d active", n)
	}
	if !passwordMatches(t, other.ID, OLD_PASSWORD) {
		t.Fatalf("expected other user's password untouched")
	}
}

// TestT31ResetWithStoredHashIsRejected pins that the stored token_hash is not itself a valid reset token.
func TestT31ResetWithStoredHashIsRejected(t *testing.T) {
	router := setupPasswordTest(t)
	user := createPasswordTestUser(t, "ana@example.com", false)
	postJSON(t, router, FORGOT_PATH, map[string]string{"email": "ana@example.com"})

	var issued models.RecoverToken
	if err := db.DB().Where("user_id = ? AND used_at IS NULL", user.ID).First(&issued).Error; err != nil {
		t.Fatalf("expected issued recover token: %v", err)
	}
	w := postJSON(t, router, RESET_PATH, map[string]string{"token": issued.TokenHash, "new_password": NEW_PASSWORD})
	assertResponse(t, w, http.StatusUnauthorized, INVALID_TOKEN_BODY)
	if !passwordMatches(t, user.ID, OLD_PASSWORD) {
		t.Fatalf("expected password unchanged")
	}
}

func TestT31ResetRejectedTokens(t *testing.T) {
	used := time.Now().Add(-time.Minute)
	cases := []struct {
		name      string
		expiresAt time.Time
		usedAt    *time.Time
		sent      string
	}{
		{"unknown token", time.Now().Add(time.Hour), nil, "does-not-exist"},
		{"expired token", time.Now().Add(-time.Second), nil, "the-token"},
		{"used token", time.Now().Add(time.Hour), &used, "the-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := setupPasswordTest(t)
			user := createPasswordTestUser(t, "ana@example.com", false)
			createRecoverToken(t, user.ID, "the-token", tc.expiresAt, tc.usedAt)
			createActiveRefreshToken(t, user.ID, "refresh-a1")

			w := postJSON(t, router, RESET_PATH, map[string]string{"token": tc.sent, "new_password": NEW_PASSWORD})

			assertResponse(t, w, http.StatusUnauthorized, INVALID_TOKEN_BODY)
			if !passwordMatches(t, user.ID, OLD_PASSWORD) {
				t.Fatalf("expected password unchanged")
			}
			if n := countActiveRefreshTokens(t, user.ID); n != 1 {
				t.Fatalf("expected refresh token untouched, got %d active", n)
			}
		})
	}
}

func TestT31ResetTokenCannotBeReused(t *testing.T) {
	router := setupPasswordTest(t)
	user := createPasswordTestUser(t, "ana@example.com", false)
	createRecoverToken(t, user.ID, "once-token", time.Now().Add(time.Hour), nil)

	first := postJSON(t, router, RESET_PATH, map[string]string{"token": "once-token", "new_password": NEW_PASSWORD})
	assertResponse(t, first, http.StatusOK, RESET_OK_BODY)

	second := postJSON(t, router, RESET_PATH, map[string]string{"token": "once-token", "new_password": "anotherpass123"})
	assertResponse(t, second, http.StatusUnauthorized, INVALID_TOKEN_BODY)
	if !passwordMatches(t, user.ID, NEW_PASSWORD) {
		t.Fatalf("expected password from first reset to remain")
	}
}

func assertInvalidRequest(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON body: %v", err)
	}
	if resp["error"] != "invalid request" {
		t.Fatalf("expected error \"invalid request\", got %v", resp["error"])
	}
	if _, ok := resp["details"].(string); !ok {
		t.Fatalf("expected string details, got %v", resp["details"])
	}
}

func TestT31ResetInvalidRequestReturns400(t *testing.T) {
	cases := map[string]string{
		"password 7 chars": `{"token":"valid-token","new_password":"1234567"}`,
		"missing password": `{"token":"valid-token"}`,
		"missing token":    `{"new_password":"newpassword123"}`,
		"empty token":      `{"token":"","new_password":"newpassword123"}`,
		"malformed":        `{"token":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			router := setupPasswordTest(t)
			user := createPasswordTestUser(t, "ana@example.com", false)
			rt := createRecoverToken(t, user.ID, "valid-token", time.Now().Add(time.Hour), nil)

			w := postRaw(router, RESET_PATH, body, nextClientAddr())

			assertInvalidRequest(t, w)
			if !passwordMatches(t, user.ID, OLD_PASSWORD) {
				t.Fatalf("expected password unchanged")
			}
			var reloaded models.RecoverToken
			db.DB().First(&reloaded, "id = ?", rt.ID)
			if reloaded.UsedAt != nil {
				t.Fatalf("expected recover token still unused")
			}
		})
	}
}

func TestT31ResetPasswordExactlyEightCharsAccepted(t *testing.T) {
	router := setupPasswordTest(t)
	user := createPasswordTestUser(t, "ana@example.com", false)
	createRecoverToken(t, user.ID, "boundary-token", time.Now().Add(time.Hour), nil)

	w := postJSON(t, router, RESET_PATH, map[string]string{"token": "boundary-token", "new_password": "12345678"})

	assertResponse(t, w, http.StatusOK, RESET_OK_BODY)
	if !passwordMatches(t, user.ID, "12345678") {
		t.Fatalf("expected 8-char password stored")
	}
}
