package twofa

// T32 — characterization tests of the current features/twofa behavior
// (decision #8). They pin what the handlers do today; the known limitations of
// specs/auth.md §10 (disabled user via verify, TOTP replay, no per-challenge
// attempt cap) are deliberately not exercised.

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
	"github.com/golang-jwt/jwt/v5"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/models"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const T32_SECRET = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
const T32_PASSWORD = "password123"

// g_T32RequestSeq gives every request its own client IP so the package-global
// rate limiter (key = IP + path) never answers 429 across tests.
var g_T32RequestSeq uint32

func t32Setup(t *testing.T) *gin.Engine {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := testDB.AutoMigrate(&models.User{}, &models.RefreshToken{}, &models.TwoFAChallenge{}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	db.SetTestDB(testDB)
	config.Load()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r.Group("/v1"))
	return r
}

// t32CreateUser seeds a user; two_fa_enabled=false is written by an explicit
// update because GORM skips zero-value fields that have a column default.
func t32CreateUser(t *testing.T, email string, enabled bool, secret *string) *models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(T32_PASSWORD), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	passwordHash := string(hash)
	user := &models.User{
		Email:        email,
		PasswordHash: &passwordHash,
		Nome:         "T32 User",
		TwoFAEnabled: enabled,
		TwoFASecret:  secret,
	}
	if err := db.DB().Create(user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if !enabled {
		db.DB().Model(user).Update("two_fa_enabled", false)
	}
	return user
}

func t32SecretPtr() *string {
	s := T32_SECRET
	return &s
}

func t32CreateChallenge(t *testing.T, userID string, expiresAt time.Time) string {
	t.Helper()
	raw := models.GenerateOpaqueToken()
	challenge := models.TwoFAChallenge{
		UserID:    userID,
		TokenHash: models.HashToken(raw),
		ExpiresAt: expiresAt,
	}
	if err := db.DB().Create(&challenge).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	return raw
}

func t32ValidCode(t *testing.T) string {
	t.Helper()
	code, err := totp.GenerateCode(T32_SECRET, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

// t32WrongCode returns a 6-digit code accepted by no step of the current
// validation window (t-30s, t, t+30s).
func t32WrongCode(t *testing.T) string {
	t.Helper()
	now := time.Now()
	valid := map[string]bool{}
	for _, offset := range []time.Duration{-60 * time.Second, -30 * time.Second, 0, 30 * time.Second, 60 * time.Second} {
		code, err := totp.GenerateCode(T32_SECRET, now.Add(offset))
		if err != nil {
			t.Fatalf("generate code: %v", err)
		}
		valid[code] = true
	}
	for i := 0; i < 1000000; i++ {
		candidate := fmt.Sprintf("%06d", i)
		if !valid[candidate] {
			return candidate
		}
	}
	t.Fatal("no wrong code found")
	return ""
}

func t32AccessToken(t *testing.T, userID string) string {
	t.Helper()
	cfg := config.Get()
	claims := jwt.MapClaims{
		"sub":          userID,
		"iat":          time.Now().Unix(),
		"exp":          time.Now().Add(5 * time.Minute).Unix(),
		"jti":          "t32-jti",
		"2fa_verified": false,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

func t32Do(t *testing.T, r *gin.Engine, path string, body interface{}, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	switch b := body.(type) {
	case nil:
		payload = nil
	case string:
		payload = []byte(b)
	default:
		encoded, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		payload = encoded
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	n := atomic.AddUint32(&g_T32RequestSeq, 1)
	req.RemoteAddr = fmt.Sprintf("10.32.%d.%d:1234", (n/256)%256, n%256)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func t32Body(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	return body
}

func t32AssertError(t *testing.T, w *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status = %d, want %d; body %s", w.Code, status, w.Body.String())
	}
	if got := t32Body(t, w)["error"]; got != message {
		t.Fatalf("error = %v, want %q", got, message)
	}
}

func t32ReloadUser(t *testing.T, id string) models.User {
	t.Helper()
	var user models.User
	if err := db.DB().First(&user, "id = ?", id).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return user
}

// --- POST /v1/auth/2fa/verify ---

func TestT32_Verify_ValidChallengeAndCode_IssuesTokensNewFamilyAndDeletesChallenge(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-verify@example.com", true, t32SecretPtr())
	oldFamily := "11111111-1111-4111-8111-111111111111"
	prior := models.RefreshToken{UserID: user.ID, TokenHash: models.HashToken("t32-prior"), FamilyID: oldFamily, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.DB().Create(&prior).Error; err != nil {
		t.Fatalf("seed prior refresh: %v", err)
	}
	challenge := t32CreateChallenge(t, user.ID, time.Now().Add(5*time.Minute))

	w := t32Do(t, r, "/v1/auth/2fa/verify", map[string]string{"challenge_token": challenge, "code": t32ValidCode(t)}, "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	body := t32Body(t, w)
	if body["token_type"] != "Bearer" {
		t.Errorf("token_type = %v, want Bearer", body["token_type"])
	}
	if want := config.Get().AccessTokenTTL.Seconds(); body["expires_in"] != want {
		t.Errorf("expires_in = %v, want %v", body["expires_in"], want)
	}
	refresh, _ := body["refresh_token"].(string)
	if refresh == "" {
		t.Fatal("refresh_token missing")
	}
	access, _ := body["access_token"].(string)
	parsed, err := jwt.Parse(access, func(*jwt.Token) (interface{}, error) { return []byte(config.Get().JWTSecret), nil })
	if err != nil || !parsed.Valid {
		t.Fatalf("access_token invalid: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["sub"] != user.ID {
		t.Errorf("sub = %v, want %s", claims["sub"], user.ID)
	}
	if claims["2fa_verified"] != true {
		t.Errorf("2fa_verified = %v, want true", claims["2fa_verified"])
	}

	var stored models.RefreshToken
	if err := db.DB().Where("token_hash = ?", models.HashToken(refresh)).First(&stored).Error; err != nil {
		t.Fatalf("issued refresh token not stored by hash: %v", err)
	}
	if stored.FamilyID == "" || stored.FamilyID == oldFamily {
		t.Errorf("family_id = %q, want a new non-empty family", stored.FamilyID)
	}
	if stored.UserID != user.ID {
		t.Errorf("refresh user_id = %s, want %s", stored.UserID, user.ID)
	}

	var remaining int64
	db.DB().Model(&models.TwoFAChallenge{}).Where("token_hash = ?", models.HashToken(challenge)).Count(&remaining)
	if remaining != 0 {
		t.Errorf("challenge rows after verify = %d, want 0", remaining)
	}
}

func TestT32_Verify_UnknownChallenge_Returns401InvalidOrExpired(t *testing.T) {
	r := t32Setup(t)
	t32CreateUser(t, "t32-unknown@example.com", true, t32SecretPtr())

	w := t32Do(t, r, "/v1/auth/2fa/verify", map[string]string{"challenge_token": "not-a-challenge", "code": t32ValidCode(t)}, "")

	t32AssertError(t, w, http.StatusUnauthorized, "invalid or expired challenge")
}

func TestT32_Verify_ExpiredChallenge_Returns401InvalidOrExpired(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-expired@example.com", true, t32SecretPtr())
	challenge := t32CreateChallenge(t, user.ID, time.Now().Add(-time.Second))

	w := t32Do(t, r, "/v1/auth/2fa/verify", map[string]string{"challenge_token": challenge, "code": t32ValidCode(t)}, "")

	t32AssertError(t, w, http.StatusUnauthorized, "invalid or expired challenge")
	var issued int64
	db.DB().Model(&models.RefreshToken{}).Where("user_id = ?", user.ID).Count(&issued)
	if issued != 0 {
		t.Errorf("refresh tokens issued = %d, want 0", issued)
	}
}

func TestT32_Verify_WrongCode_Returns401InvalidCode(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-wrongcode@example.com", true, t32SecretPtr())
	challenge := t32CreateChallenge(t, user.ID, time.Now().Add(5*time.Minute))

	w := t32Do(t, r, "/v1/auth/2fa/verify", map[string]string{"challenge_token": challenge, "code": t32WrongCode(t)}, "")

	t32AssertError(t, w, http.StatusUnauthorized, "invalid code")
	var issued int64
	db.DB().Model(&models.RefreshToken{}).Where("user_id = ?", user.ID).Count(&issued)
	if issued != 0 {
		t.Errorf("refresh tokens issued = %d, want 0", issued)
	}
}

func TestT32_Verify_UserWithout2FA_Returns401NotEnabled(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-no2fa@example.com", false, t32SecretPtr())
	challenge := t32CreateChallenge(t, user.ID, time.Now().Add(5*time.Minute))

	w := t32Do(t, r, "/v1/auth/2fa/verify", map[string]string{"challenge_token": challenge, "code": t32ValidCode(t)}, "")

	t32AssertError(t, w, http.StatusUnauthorized, "2FA not enabled")
}

func TestT32_Verify_MissingFields_Returns400InvalidRequest(t *testing.T) {
	r := t32Setup(t)

	for name, body := range map[string]interface{}{
		"no code":      map[string]string{"challenge_token": "x"},
		"no challenge": map[string]string{"code": "123456"},
		"malformed":    "{not json",
	} {
		t.Run(name, func(t *testing.T) {
			w := t32Do(t, r, "/v1/auth/2fa/verify", body, "")
			t32AssertError(t, w, http.StatusBadRequest, "invalid request")
		})
	}
}

// --- POST /v1/me/2fa/enable ---

func TestT32_Enable_WithoutCode_StoresSecretAndReturnsIt(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-setup@example.com", false, nil)

	w := t32Do(t, r, "/v1/me/2fa/enable", nil, t32AccessToken(t, user.ID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	body := t32Body(t, w)
	secret, _ := body["secret"].(string)
	if secret == "" {
		t.Fatal("secret missing")
	}
	if body["message"] != "scan with authenticator app, then POST code to confirm" {
		t.Errorf("message = %v", body["message"])
	}
	reloaded := t32ReloadUser(t, user.ID)
	if reloaded.TwoFASecret == nil || *reloaded.TwoFASecret != secret {
		t.Errorf("stored secret = %v, want %q", reloaded.TwoFASecret, secret)
	}
	if reloaded.TwoFAEnabled {
		t.Error("two_fa_enabled = true after setup without code, want false")
	}
}

func TestT32_Enable_WithValidCode_Sets2FAEnabled(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-enable@example.com", false, t32SecretPtr())

	w := t32Do(t, r, "/v1/me/2fa/enable", map[string]string{"code": t32ValidCode(t)}, t32AccessToken(t, user.ID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	if msg := t32Body(t, w)["message"]; msg != "2FA enabled" {
		t.Errorf("message = %v, want \"2FA enabled\"", msg)
	}
	if !t32ReloadUser(t, user.ID).TwoFAEnabled {
		t.Error("two_fa_enabled = false, want true")
	}
}

func TestT32_Enable_WithWrongCode_Returns401AndStaysDisabled(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-enable-wrong@example.com", false, t32SecretPtr())

	w := t32Do(t, r, "/v1/me/2fa/enable", map[string]string{"code": t32WrongCode(t)}, t32AccessToken(t, user.ID))

	t32AssertError(t, w, http.StatusUnauthorized, "invalid code")
	if t32ReloadUser(t, user.ID).TwoFAEnabled {
		t.Error("two_fa_enabled = true after wrong code, want false")
	}
}

func TestT32_Enable_WithCodeBeforeSetup_Returns400(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-enable-nosetup@example.com", false, nil)

	w := t32Do(t, r, "/v1/me/2fa/enable", map[string]string{"code": "123456"}, t32AccessToken(t, user.ID))

	t32AssertError(t, w, http.StatusBadRequest, "setup 2FA first by calling without code")
}

func TestT32_Enable_AlreadyEnabled_Returns400(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-enable-twice@example.com", true, t32SecretPtr())

	w := t32Do(t, r, "/v1/me/2fa/enable", map[string]string{"code": t32ValidCode(t)}, t32AccessToken(t, user.ID))

	t32AssertError(t, w, http.StatusBadRequest, "2FA already enabled")
	reloaded := t32ReloadUser(t, user.ID)
	if reloaded.TwoFASecret == nil || *reloaded.TwoFASecret != T32_SECRET {
		t.Error("secret changed on already-enabled user")
	}
}

// --- POST /v1/me/2fa/disable ---

func TestT32_Disable_WithCorrectPassword_Clears2FA(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-disable-pw@example.com", true, t32SecretPtr())

	w := t32Do(t, r, "/v1/me/2fa/disable", map[string]string{"password": T32_PASSWORD}, t32AccessToken(t, user.ID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	if msg := t32Body(t, w)["message"]; msg != "2FA disabled" {
		t.Errorf("message = %v, want \"2FA disabled\"", msg)
	}
	reloaded := t32ReloadUser(t, user.ID)
	if reloaded.TwoFAEnabled || reloaded.TwoFASecret != nil {
		t.Errorf("after disable: enabled=%v secret=%v, want false/nil", reloaded.TwoFAEnabled, reloaded.TwoFASecret)
	}
}

func TestT32_Disable_WithCorrectCode_Clears2FA(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-disable-code@example.com", true, t32SecretPtr())

	w := t32Do(t, r, "/v1/me/2fa/disable", map[string]string{"code": t32ValidCode(t)}, t32AccessToken(t, user.ID))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	reloaded := t32ReloadUser(t, user.ID)
	if reloaded.TwoFAEnabled || reloaded.TwoFASecret != nil {
		t.Errorf("after disable: enabled=%v secret=%v, want false/nil", reloaded.TwoFAEnabled, reloaded.TwoFASecret)
	}
}

func TestT32_Disable_NeitherCredentialValid_Returns401AndKeeps2FA(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-disable-bad@example.com", true, t32SecretPtr())

	w := t32Do(t, r, "/v1/me/2fa/disable", map[string]string{"password": "wrong-password", "code": t32WrongCode(t)}, t32AccessToken(t, user.ID))

	t32AssertError(t, w, http.StatusUnauthorized, "invalid credentials")
	reloaded := t32ReloadUser(t, user.ID)
	if !reloaded.TwoFAEnabled || reloaded.TwoFASecret == nil || *reloaded.TwoFASecret != T32_SECRET {
		t.Errorf("2FA state changed after failed disable: enabled=%v secret=%v", reloaded.TwoFAEnabled, reloaded.TwoFASecret)
	}
}

func TestT32_Disable_NoPasswordNorCode_Returns400(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-disable-empty@example.com", true, t32SecretPtr())

	w := t32Do(t, r, "/v1/me/2fa/disable", map[string]string{}, t32AccessToken(t, user.ID))

	t32AssertError(t, w, http.StatusBadRequest, "password or code required")
}

func TestT32_Disable_NotEnabled_Returns400(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-disable-off@example.com", false, nil)

	w := t32Do(t, r, "/v1/me/2fa/disable", map[string]string{"password": T32_PASSWORD}, t32AccessToken(t, user.ID))

	t32AssertError(t, w, http.StatusBadRequest, "2FA not enabled")
}

// --- JWT protection of /v1/me/2fa/* ---

func TestT32_MeRoutes_WithoutJWT_Return401(t *testing.T) {
	r := t32Setup(t)
	user := t32CreateUser(t, "t32-nojwt@example.com", true, t32SecretPtr())

	for _, path := range []string{"/v1/me/2fa/enable", "/v1/me/2fa/disable"} {
		t.Run(path, func(t *testing.T) {
			w := t32Do(t, r, path, map[string]string{"password": T32_PASSWORD}, "")
			t32AssertError(t, w, http.StatusUnauthorized, "missing authorization header")
		})
	}
	reloaded := t32ReloadUser(t, user.ID)
	if !reloaded.TwoFAEnabled || reloaded.TwoFASecret == nil {
		t.Error("2FA state changed by unauthenticated request")
	}
}
