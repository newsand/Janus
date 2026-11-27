package auth

// T29 — characterization tests: they pin the CURRENT behavior of features/auth
// (status, exact body, persisted rows) so the T35 refactor cannot move it.
// They pass at HEAD by design. Odd current behavior is pinned, not fixed.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/models"
)

// CHAR_REMOTE_ADDR keeps T29 requests in their own rate-limit bucket (the
// limiter is a package-global map keyed by client IP + path).
const CHAR_REMOTE_ADDR = "10.29.0.1:2929"

const CHAR_PASSWORD = "password123"

var g_CharHex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// charSetup gives a fresh DB, a router, and lifts rate limit/lockout for the
// test, restoring the shared config afterwards.
func charSetup(t *testing.T) (*gin.Engine, *config.Config) {
	t.Helper()
	setupTestDB(t)
	cfg := config.Load()
	prevRate, prevLock := cfg.RateLimitRequests, cfg.LockoutThreshold
	cfg.RateLimitRequests = 100000
	cfg.LockoutThreshold = 100000
	t.Cleanup(func() {
		cfg.RateLimitRequests = prevRate
		cfg.LockoutThreshold = prevLock
	})
	return setupTestRouter(), cfg
}

func charDo(router *gin.Engine, method, path string, body any, bearer string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	switch b := body.(type) {
	case nil:
	case string:
		buf.WriteString(b)
	default:
		_ = json.NewEncoder(&buf).Encode(b)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = CHAR_REMOTE_ADDR
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func charLogin(router *gin.Engine, email, password string) *httptest.ResponseRecorder {
	return charDo(router, http.MethodPost, "/v1/auth/login", map[string]string{"email": email, "password": password}, "")
}

func charDecode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not a JSON object: %q", w.Body.String())
	}
	return m
}

func charKeys(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func charExpectBody(t *testing.T, w *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if w.Code != status || w.Body.String() != body {
		t.Fatalf("expected %d %s, got %d %s", status, body, w.Code, w.Body.String())
	}
}

// charAssertTokenResponse pins the token response of login/refresh/consume:
// exact keys, Bearer, expires_in = access TTL, closed JWT claims, opaque
// 64-hex refresh stored only as its hash. Returns the response and the
// stored refresh row.
func charAssertTokenResponse(t *testing.T, w *httptest.ResponseRecorder, cfg *config.Config, userID string) (map[string]any, models.RefreshToken) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 token response, got %d %s", w.Code, w.Body.String())
	}
	resp := charDecode(t, w)
	if got := charKeys(resp); got != "access_token,expires_in,refresh_token,token_type" {
		t.Fatalf("token response keys = %s", got)
	}
	if resp["token_type"] != "Bearer" {
		t.Errorf("token_type = %v", resp["token_type"])
	}
	if resp["expires_in"] != cfg.AccessTokenTTL.Seconds() {
		t.Errorf("expires_in = %v, want %v", resp["expires_in"], cfg.AccessTokenTTL.Seconds())
	}

	claims := jwt.MapClaims{}
	tok, err := jwt.ParseWithClaims(resp["access_token"].(string), claims, func(tk *jwt.Token) (any, error) {
		return []byte(cfg.JWTSecret), nil
	})
	if err != nil || !tok.Valid {
		t.Fatalf("access token does not verify with JWT_SECRET: %v", err)
	}
	if tok.Method.Alg() != "HS256" {
		t.Errorf("alg = %s, want HS256", tok.Method.Alg())
	}
	if got := charKeys(claims); got != "2fa_verified,exp,iat,jti,sub" {
		t.Errorf("claims = %s, want closed set 2fa_verified,exp,iat,jti,sub", got)
	}
	if claims["sub"] != userID {
		t.Errorf("sub = %v, want %s", claims["sub"], userID)
	}
	if claims["2fa_verified"] != true {
		t.Errorf("2fa_verified = %v, want true", claims["2fa_verified"])
	}
	if jti, _ := claims["jti"].(string); jti == "" {
		t.Errorf("jti is empty")
	}
	if exp, iat := claims["exp"].(float64), claims["iat"].(float64); exp-iat != cfg.AccessTokenTTL.Seconds() {
		t.Errorf("exp-iat = %v, want %v", exp-iat, cfg.AccessTokenTTL.Seconds())
	}

	raw, _ := resp["refresh_token"].(string)
	if !g_CharHex64.MatchString(raw) {
		t.Fatalf("refresh_token is not 64 hex: %q", raw)
	}
	var row models.RefreshToken
	if err := db.DB().Where("token_hash = ?", models.HashToken(raw)).First(&row).Error; err != nil {
		t.Fatalf("refresh token hash not persisted: %v", err)
	}
	var rawStored int64
	db.DB().Model(&models.RefreshToken{}).Where("token_hash = ?", raw).Count(&rawStored)
	if rawStored != 0 {
		t.Errorf("raw refresh token stored")
	}
	if row.UserID != userID || row.RevokedAt != nil || row.FamilyID == "" {
		t.Errorf("refresh row = user %s revoked %v family %q", row.UserID, row.RevokedAt, row.FamilyID)
	}
	if d := time.Until(row.ExpiresAt) - cfg.RefreshTokenTTL; d > time.Minute || d < -time.Minute {
		t.Errorf("refresh expires_at off by %v from REFRESH_TOKEN_TTL", d)
	}
	return resp, row
}

func charEnable2FA(t *testing.T, user *models.User) {
	t.Helper()
	secret := "JBSWY3DPEHPK3PXP"
	if err := db.DB().Model(user).Updates(map[string]any{"two_fa_enabled": true, "two_fa_secret": secret}).Error; err != nil {
		t.Fatalf("enable 2fa: %v", err)
	}
}

func charCountRefresh(userID string) int64 {
	var n int64
	db.DB().Model(&models.RefreshToken{}).Where("user_id = ?", userID).Count(&n)
	return n
}

func charInsertMagic(t *testing.T, userID string, expiresAt time.Time) string {
	t.Helper()
	raw := models.GenerateOpaqueToken()
	m := models.MagicToken{UserID: userID, TokenHash: models.HashToken(raw), ExpiresAt: expiresAt}
	if err := db.DB().Create(&m).Error; err != nil {
		t.Fatalf("insert magic token: %v", err)
	}
	return raw
}

// --- Login ---

func TestCharT29LoginSuccessReturnsTokenResponse(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-login@example.com", CHAR_PASSWORD)

	charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
}

func TestCharT29LoginEachSuccessStartsNewFamily(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-family@example.com", CHAR_PASSWORD)

	_, first := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
	_, second := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
	if first.FamilyID == second.FamilyID {
		t.Errorf("two logins share family %s", first.FamilyID)
	}
}

func TestCharT29LoginWith2FAReturnsChallengeWithoutTokens(t *testing.T) {
	router, _ := charSetup(t)
	user := createTestUser(t, "t29-2fa@example.com", CHAR_PASSWORD)
	charEnable2FA(t, user)

	w := charLogin(router, user.Email, CHAR_PASSWORD)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", w.Code, w.Body.String())
	}
	resp := charDecode(t, w)
	if got := charKeys(resp); got != "challenge_token,requires_2fa" {
		t.Fatalf("2fa login keys = %s (no access/refresh token expected)", got)
	}
	if resp["requires_2fa"] != true {
		t.Errorf("requires_2fa = %v", resp["requires_2fa"])
	}
	challenge, _ := resp["challenge_token"].(string)
	if !g_CharHex64.MatchString(challenge) {
		t.Fatalf("challenge_token is not 64 hex: %q", challenge)
	}
	var row models.TwoFAChallenge
	if err := db.DB().Where("token_hash = ?", models.HashToken(challenge)).First(&row).Error; err != nil {
		t.Fatalf("challenge hash not persisted: %v", err)
	}
	if row.UserID != user.ID {
		t.Errorf("challenge user = %s", row.UserID)
	}
	if d := time.Until(row.ExpiresAt) - 5*time.Minute; d > time.Minute || d < -time.Minute {
		t.Errorf("challenge TTL off by %v from 5m", d)
	}
	if n := charCountRefresh(user.ID); n != 0 {
		t.Errorf("2fa login created %d refresh tokens", n)
	}
}

func TestCharT29LoginWith2FAReplacesPreviousChallenge(t *testing.T) {
	router, _ := charSetup(t)
	user := createTestUser(t, "t29-2fa-replace@example.com", CHAR_PASSWORD)
	charEnable2FA(t, user)

	charLogin(router, user.Email, CHAR_PASSWORD)
	charLogin(router, user.Email, CHAR_PASSWORD)

	var n int64
	db.DB().Model(&models.TwoFAChallenge{}).Where("user_id = ?", user.ID).Count(&n)
	if n != 1 {
		t.Errorf("expected 1 challenge after two logins, got %d", n)
	}
}

func TestCharT29Login2FAEnabledWithoutSecretIssuesTokens(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-2fa-nosecret@example.com", CHAR_PASSWORD)
	db.DB().Model(user).Update("two_fa_enabled", true)

	charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
}

func TestCharT29LoginMalformedBodyIs400(t *testing.T) {
	router, _ := charSetup(t)

	for name, body := range map[string]any{
		"not json":      "{",
		"missing pass":  map[string]string{"email": "t29@example.com"},
		"invalid email": map[string]string{"email": "nope", "password": CHAR_PASSWORD},
	} {
		w := charDo(router, http.MethodPost, "/v1/auth/login", body, "")
		if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"invalid request"}` {
			t.Errorf("%s: got %d %s", name, w.Code, w.Body.String())
		}
	}
}

// --- Refresh ---

func TestCharT29RefreshOkRotatesWithinFamily(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-refresh@example.com", CHAR_PASSWORD)
	login, first := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)

	w := charDo(router, http.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": login["refresh_token"].(string)}, "")
	_, second := charAssertTokenResponse(t, w, cfg, user.ID)
	if second.FamilyID != first.FamilyID {
		t.Errorf("rotation changed family %s -> %s", first.FamilyID, second.FamilyID)
	}
	var old models.RefreshToken
	db.DB().First(&old, "id = ?", first.ID)
	if old.RevokedAt == nil {
		t.Errorf("presented refresh token not revoked")
	}
}

func TestCharT29RefreshUnknownTokenIs401(t *testing.T) {
	router, _ := charSetup(t)

	w := charDo(router, http.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": models.GenerateOpaqueToken()}, "")
	charExpectBody(t, w, http.StatusUnauthorized, `{"error":"invalid refresh token"}`)
}

func TestCharT29RefreshExpiredTokenIs401Expired(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-refresh-exp@example.com", CHAR_PASSWORD)
	login, row := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
	db.DB().Model(&models.RefreshToken{}).Where("id = ?", row.ID).Update("expires_at", time.Now().Add(-time.Minute))

	w := charDo(router, http.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": login["refresh_token"].(string)}, "")
	charExpectBody(t, w, http.StatusUnauthorized, `{"error":"refresh token expired"}`)
}

func TestCharT29RefreshDisabledUserBodyIsInvalidRefreshToken(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-refresh-dis@example.com", CHAR_PASSWORD)
	login, _ := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
	db.DB().Model(user).Update("disabled_at", time.Now())

	w := charDo(router, http.MethodPost, "/v1/auth/refresh", map[string]string{"refresh_token": login["refresh_token"].(string)}, "")
	charExpectBody(t, w, http.StatusUnauthorized, `{"error":"invalid refresh token"}`)
}

func TestCharT29RefreshReuseBodyIsInvalidRefreshToken(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-refresh-reuse@example.com", CHAR_PASSWORD)
	login, _ := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
	body := map[string]string{"refresh_token": login["refresh_token"].(string)}
	charDo(router, http.MethodPost, "/v1/auth/refresh", body, "")

	w := charDo(router, http.MethodPost, "/v1/auth/refresh", body, "")
	charExpectBody(t, w, http.StatusUnauthorized, `{"error":"invalid refresh token"}`)
}

func TestCharT29RefreshMalformedBodyIs400(t *testing.T) {
	router, _ := charSetup(t)

	w := charDo(router, http.MethodPost, "/v1/auth/refresh", map[string]string{}, "")
	charExpectBody(t, w, http.StatusBadRequest, `{"error":"invalid request"}`)
}

// --- Logout ---

func TestCharT29LogoutRevokesAllUserRefreshTokens(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-logout@example.com", CHAR_PASSWORD)
	other := createTestUser(t, "t29-logout-other@example.com", CHAR_PASSWORD)
	login, _ := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
	charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
	charAssertTokenResponse(t, charLogin(router, other.Email, CHAR_PASSWORD), cfg, other.ID)

	w := charDo(router, http.MethodPost, "/v1/auth/logout", nil, login["access_token"].(string))
	charExpectBody(t, w, http.StatusOK, `{"message":"logged out"}`)

	var active int64
	db.DB().Model(&models.RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Count(&active)
	if active != 0 {
		t.Errorf("user still has %d active refresh tokens", active)
	}
	db.DB().Model(&models.RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", other.ID).Count(&active)
	if active != 1 {
		t.Errorf("other user's refresh tokens touched: %d active", active)
	}
}

func TestCharT29LogoutWithoutJWTIs401(t *testing.T) {
	router, _ := charSetup(t)

	w := charDo(router, http.MethodPost, "/v1/auth/logout", nil, "")
	charExpectBody(t, w, http.StatusUnauthorized, `{"error":"missing authorization header"}`)
}

// --- GetMe ---

func TestCharT29GetMeReturnsClosedShape(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-me@example.com", CHAR_PASSWORD)
	login, _ := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)

	w := charDo(router, http.MethodGet, "/v1/me", nil, login["access_token"].(string))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", w.Code, w.Body.String())
	}
	resp := charDecode(t, w)
	if got := charKeys(resp); got != "2fa_enabled,email,id,nome" {
		t.Fatalf("/me keys = %s", got)
	}
	if resp["id"] != user.ID || resp["email"] != user.Email || resp["nome"] != "Test User" || resp["2fa_enabled"] != false {
		t.Errorf("/me = %v", resp)
	}
}

func TestCharT29GetMeWithoutOrBadJWTIs401(t *testing.T) {
	router, _ := charSetup(t)

	charExpectBody(t, charDo(router, http.MethodGet, "/v1/me", nil, ""), http.StatusUnauthorized, `{"error":"missing authorization header"}`)
	charExpectBody(t, charDo(router, http.MethodGet, "/v1/me", nil, "not-a-jwt"), http.StatusUnauthorized, `{"error":"invalid token"}`)
}

// Pins current behavior: JWT valid but user row gone → 500 internal error.
func TestCharT29GetMeUnknownSubjectIs500(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-me-gone@example.com", CHAR_PASSWORD)
	login, _ := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
	db.DB().Unscoped().Delete(&models.User{}, "id = ?", user.ID)

	w := charDo(router, http.MethodGet, "/v1/me", nil, login["access_token"].(string))
	charExpectBody(t, w, http.StatusInternalServerError, `{"error":"internal error"}`)
}

// --- RequestMagicLink ---

const CHAR_MAGIC_GENERIC = `{"message":"if the email exists, a magic link will be sent"}`

func TestCharT29RequestMagicLinkAlwaysGeneric(t *testing.T) {
	router, _ := charSetup(t)
	active := createTestUser(t, "t29-magic@example.com", CHAR_PASSWORD)
	disabled := createTestUser(t, "t29-magic-dis@example.com", CHAR_PASSWORD)
	db.DB().Model(disabled).Update("disabled_at", time.Now())
	pending := &models.User{Email: "t29-magic-pending@example.com", Nome: "Pending"}
	db.DB().Create(pending)

	for name, body := range map[string]any{
		"existing":    map[string]string{"email": active.Email},
		"unknown":     map[string]string{"email": "t29-ghost@example.com"},
		"disabled":    map[string]string{"email": disabled.Email},
		"no password": map[string]string{"email": pending.Email},
		"malformed":   "{",
		"bad email":   map[string]string{"email": "nope"},
	} {
		w := charDo(router, http.MethodPost, "/v1/auth/magic-link", body, "")
		if w.Code != http.StatusOK || w.Body.String() != CHAR_MAGIC_GENERIC {
			t.Errorf("%s: got %d %s", name, w.Code, w.Body.String())
		}
	}

	for user, want := range map[*models.User]int64{active: 1, disabled: 0, pending: 0} {
		var n int64
		db.DB().Model(&models.MagicToken{}).Where("user_id = ?", user.ID).Count(&n)
		if n != want {
			t.Errorf("%s: %d magic tokens, want %d", user.Email, n, want)
		}
	}
	var total int64
	db.DB().Model(&models.MagicToken{}).Count(&total)
	if total != 1 {
		t.Errorf("total magic tokens = %d, want 1", total)
	}
}

func TestCharT29RequestMagicLinkInvalidatesPreviousUnused(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-magic-again@example.com", CHAR_PASSWORD)
	old := charInsertMagic(t, user.ID, time.Now().Add(time.Hour))

	charExpectBody(t, charDo(router, http.MethodPost, "/v1/auth/magic-link", map[string]string{"email": user.Email}, ""), http.StatusOK, CHAR_MAGIC_GENERIC)

	var oldRow models.MagicToken
	db.DB().Where("token_hash = ?", models.HashToken(old)).First(&oldRow)
	if oldRow.UsedAt == nil {
		t.Errorf("previous unused magic token not invalidated")
	}
	var fresh models.MagicToken
	if err := db.DB().Where("user_id = ? AND used_at IS NULL", user.ID).First(&fresh).Error; err != nil {
		t.Fatalf("no new unused magic token: %v", err)
	}
	if d := time.Until(fresh.ExpiresAt) - cfg.MagicLinkTTL; d > time.Minute || d < -time.Minute {
		t.Errorf("magic TTL off by %v from MAGIC_LINK_TTL", d)
	}
}

// --- ConsumeMagicLink ---

func TestCharT29ConsumeMagicLinkOkStartsNewFamily(t *testing.T) {
	router, cfg := charSetup(t)
	user := createTestUser(t, "t29-consume@example.com", CHAR_PASSWORD)
	_, loginRow := charAssertTokenResponse(t, charLogin(router, user.Email, CHAR_PASSWORD), cfg, user.ID)
	raw := charInsertMagic(t, user.ID, time.Now().Add(time.Hour))

	w := charDo(router, http.MethodPost, "/v1/auth/magic-link/consume", map[string]string{"token": raw}, "")
	_, row := charAssertTokenResponse(t, w, cfg, user.ID)
	if row.FamilyID == loginRow.FamilyID {
		t.Errorf("magic link consume reused login family %s", row.FamilyID)
	}
	var magic models.MagicToken
	db.DB().Where("token_hash = ?", models.HashToken(raw)).First(&magic)
	if magic.UsedAt == nil {
		t.Errorf("consumed magic token not marked used")
	}

	w = charDo(router, http.MethodPost, "/v1/auth/magic-link/consume", map[string]string{"token": raw}, "")
	charExpectBody(t, w, http.StatusUnauthorized, `{"error":"invalid or expired token"}`)
}

func TestCharT29ConsumeMagicLinkWith2FAReturnsChallenge(t *testing.T) {
	router, _ := charSetup(t)
	user := createTestUser(t, "t29-consume-2fa@example.com", CHAR_PASSWORD)
	charEnable2FA(t, user)
	raw := charInsertMagic(t, user.ID, time.Now().Add(time.Hour))

	w := charDo(router, http.MethodPost, "/v1/auth/magic-link/consume", map[string]string{"token": raw}, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", w.Code, w.Body.String())
	}
	resp := charDecode(t, w)
	if got := charKeys(resp); got != "challenge_token,requires_2fa" || resp["requires_2fa"] != true {
		t.Fatalf("consume with 2fa = %v", resp)
	}
	if n := charCountRefresh(user.ID); n != 0 {
		t.Errorf("2fa consume created %d refresh tokens", n)
	}
}

func TestCharT29ConsumeMagicLinkInvalidOrExpiredIs401(t *testing.T) {
	router, _ := charSetup(t)
	user := createTestUser(t, "t29-consume-bad@example.com", CHAR_PASSWORD)
	expired := charInsertMagic(t, user.ID, time.Now().Add(-time.Minute))

	for name, token := range map[string]string{
		"unknown": models.GenerateOpaqueToken(),
		"expired": expired,
	} {
		w := charDo(router, http.MethodPost, "/v1/auth/magic-link/consume", map[string]string{"token": token}, "")
		if w.Code != http.StatusUnauthorized || w.Body.String() != `{"error":"invalid or expired token"}` {
			t.Errorf("%s: got %d %s", name, w.Code, w.Body.String())
		}
	}
	if n := charCountRefresh(user.ID); n != 0 {
		t.Errorf("failed consume created %d refresh tokens", n)
	}
}

// Pins current behavior: the token is marked used BEFORE the user check, so a
// disabled user's attempt burns the magic token.
func TestCharT29ConsumeMagicLinkDisabledUserIs401AndBurnsToken(t *testing.T) {
	router, _ := charSetup(t)
	user := createTestUser(t, "t29-consume-dis@example.com", CHAR_PASSWORD)
	raw := charInsertMagic(t, user.ID, time.Now().Add(time.Hour))
	db.DB().Model(user).Update("disabled_at", time.Now())

	w := charDo(router, http.MethodPost, "/v1/auth/magic-link/consume", map[string]string{"token": raw}, "")
	charExpectBody(t, w, http.StatusUnauthorized, `{"error":"invalid or expired token"}`)

	var magic models.MagicToken
	db.DB().Where("token_hash = ?", models.HashToken(raw)).First(&magic)
	if magic.UsedAt == nil {
		t.Errorf("current behavior: disabled-user consume marks the token used")
	}
	if n := charCountRefresh(user.ID); n != 0 {
		t.Errorf("disabled consume created %d refresh tokens", n)
	}
}

func TestCharT29ConsumeMagicLinkMalformedBodyIs400(t *testing.T) {
	router, _ := charSetup(t)

	w := charDo(router, http.MethodPost, "/v1/auth/magic-link/consume", map[string]string{}, "")
	charExpectBody(t, w, http.StatusBadRequest, `{"error":"invalid request"}`)
}
