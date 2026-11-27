package users

// T34 — characterization tests: they pin the CURRENT behavior of the users
// feature (GetUser, UpdateUser, CreateUser, CreateInvite, AcceptInvite) before
// the T36 refactor. Expected to PASS on HEAD. Odd behavior is recorded, not
// fixed. Helpers are prefixed `charUsers` so they never clash with
// handler_test.go (which is left untouched).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/mailer"
	"github.com/newsand/janus/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const charUsersServiceKey = "t34-char-service-key"

type charUsersResponse struct {
	code int
	body map[string]interface{}
	raw  string
}

func charUsersSetup(t *testing.T) *gin.Engine {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := testDB.AutoMigrate(
		&models.User{},
		&models.RefreshToken{},
		&models.Invite{},
		&models.MagicToken{},
		&models.TwoFAChallenge{},
		&mailer.Outbox{},
	); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	db.SetTestDB(testDB)

	cfg := config.Load()
	prevKeys, prevStub := cfg.ServiceKeys, cfg.MailerStub
	cfg.ServiceKeys = []string{charUsersServiceKey}
	cfg.MailerStub = true
	mailer.Init(cfg)
	t.Cleanup(func() {
		cfg.ServiceKeys, cfg.MailerStub = prevKeys, prevStub
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r.Group("/v1"))
	return r
}

// charUsersDo sends a request. auth "" omits the Authorization header;
// remoteAddr "" keeps the httptest default (used to isolate the rate-limit
// bucket of /v1/invites/accept from the other tests of the package).
func charUsersDo(t *testing.T, r *gin.Engine, method, path string, body interface{}, auth, remoteAddr string) charUsersResponse {
	t.Helper()
	var buf *bytes.Buffer
	switch b := body.(type) {
	case nil:
		buf = &bytes.Buffer{}
	case string:
		buf = bytes.NewBufferString(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		buf = bytes.NewBuffer(raw)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	out := charUsersResponse{code: w.Code, raw: w.Body.String()}
	if err := json.Unmarshal(w.Body.Bytes(), &out.body); err != nil {
		t.Fatalf("decode %d %q: %v", w.Code, out.raw, err)
	}
	return out
}

func charUsersBearer() string { return "Bearer " + charUsersServiceKey }

func charUsersCreateUser(t *testing.T, u *models.User) *models.User {
	t.Helper()
	if err := db.DB().Create(u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func charUsersCreateRefresh(t *testing.T, userID, hash string, revokedAt *time.Time) *models.RefreshToken {
	t.Helper()
	rt := &models.RefreshToken{
		UserID:    userID,
		TokenHash: hash,
		FamilyID:  uuid.New().String(),
		ExpiresAt: time.Now().Add(time.Hour),
		RevokedAt: revokedAt,
	}
	if err := db.DB().Create(rt).Error; err != nil {
		t.Fatalf("create refresh token: %v", err)
	}
	return rt
}

func charUsersCreateInvite(t *testing.T, inv *models.Invite) {
	t.Helper()
	if err := db.DB().Create(inv).Error; err != nil {
		t.Fatalf("create invite: %v", err)
	}
}

func charUsersActiveRefresh(t *testing.T, userID string) int64 {
	t.Helper()
	var n int64
	db.DB().Model(&models.RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", userID).Count(&n)
	return n
}

func charUsersReload(t *testing.T, id string) models.User {
	t.Helper()
	var u models.User
	if err := db.DB().First(&u, "id = ?", id).Error; err != nil {
		t.Fatalf("reload user %s: %v", id, err)
	}
	return u
}

func charUsersStrPtr(s string) *string { return &s }

func charUsersExpectError(t *testing.T, got charUsersResponse, code int, errMsg string) {
	t.Helper()
	if got.code != code || got.body["error"] != errMsg {
		t.Fatalf("expected %d {error:%q}, got %d %s", code, errMsg, got.code, got.raw)
	}
}

// ---------------------------------------------------------------- GetUser

func TestT34_GetUser_200ReturnsProfileFields(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{
		Email:    "get@example.com",
		Nome:     "Get User",
		Telefone: charUsersStrPtr("+5511999990000"),
		CPF:      charUsersStrPtr("123.456.789-09"),
	})

	got := charUsersDo(t, r, "GET", "/v1/users/"+u.ID, nil, charUsersBearer(), "")
	if got.code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", got.code, got.raw)
	}
	want := map[string]interface{}{
		"id":          u.ID,
		"email":       "get@example.com",
		"nome":        "Get User",
		"telefone":    "+5511999990000",
		"cpf":         "123.456.789-09",
		"2fa_enabled": false,
		"disabled_at": nil,
	}
	for k, v := range want {
		if got.body[k] != v {
			t.Errorf("field %q = %#v, want %#v", k, got.body[k], v)
		}
	}
	for _, k := range []string{"created_at", "updated_at"} {
		if s, ok := got.body[k].(string); !ok || s == "" {
			t.Errorf("field %q missing or empty: %#v", k, got.body[k])
		}
	}
	keys := map[string]bool{"id": true, "email": true, "nome": true, "telefone": true, "cpf": true,
		"2fa_enabled": true, "created_at": true, "updated_at": true, "disabled_at": true}
	for k := range got.body {
		if !keys[k] {
			t.Errorf("unexpected field %q in GET /users/:id response (password_hash/totp must never leak)", k)
		}
	}
	if len(got.body) != len(keys) {
		t.Errorf("expected exactly %d fields, got %d: %s", len(keys), len(got.body), got.raw)
	}
}

func TestT34_GetUser_NullOptionalFieldsAndDisabledAt(t *testing.T) {
	r := charUsersSetup(t)
	now := time.Now()
	u := charUsersCreateUser(t, &models.User{Email: "nulls@example.com", Nome: "Nulls", DisabledAt: &now})

	got := charUsersDo(t, r, "GET", "/v1/users/"+u.ID, nil, charUsersBearer(), "")
	if got.code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", got.code, got.raw)
	}
	if got.body["telefone"] != nil || got.body["cpf"] != nil {
		t.Errorf("telefone/cpf not set must be null, got %#v / %#v", got.body["telefone"], got.body["cpf"])
	}
	if s, ok := got.body["disabled_at"].(string); !ok || s == "" {
		t.Errorf("disabled user must expose disabled_at timestamp, got %#v", got.body["disabled_at"])
	}
}

func TestT34_GetUser_404UnknownID(t *testing.T) {
	r := charUsersSetup(t)
	for _, id := range []string{uuid.New().String(), "not-a-uuid", "' OR '1'='1"} {
		got := charUsersDo(t, r, "GET", "/v1/users/"+url.PathEscape(id), nil, charUsersBearer(), "")
		charUsersExpectError(t, got, http.StatusNotFound, "user not found")
	}
}

func TestT34_GetUser_401WithoutValidServiceKey(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "nokey@example.com", Nome: "No Key"})
	path := "/v1/users/" + u.ID

	charUsersExpectError(t, charUsersDo(t, r, "GET", path, nil, "", ""), http.StatusUnauthorized, "missing authorization header")
	charUsersExpectError(t, charUsersDo(t, r, "GET", path, nil, charUsersServiceKey, ""), http.StatusUnauthorized, "invalid authorization header")
	charUsersExpectError(t, charUsersDo(t, r, "GET", path, nil, "Bearer wrong-key", ""), http.StatusUnauthorized, "invalid service key")
}

// ------------------------------------------------------------- UpdateUser

func TestT34_UpdateUser_200NomeTelefonePersistedNoRevocation(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "upd@example.com", Nome: "Before"})
	charUsersCreateRefresh(t, u.ID, "t34-upd-h1", nil)

	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID,
		map[string]interface{}{"nome": "  After  ", "telefone": "+5521888887777"}, charUsersBearer(), "")
	if got.code != http.StatusOK || got.body["message"] != "user updated" || len(got.body) != 1 {
		t.Fatalf("expected 200 {message:user updated}, got %d %s", got.code, got.raw)
	}
	after := charUsersReload(t, u.ID)
	if after.Nome != "After" {
		t.Errorf("nome = %q, want trimmed %q", after.Nome, "After")
	}
	if after.Telefone == nil || *after.Telefone != "+5521888887777" {
		t.Errorf("telefone = %v, want +5521888887777", after.Telefone)
	}
	if after.Email != "upd@example.com" {
		t.Errorf("email must be untouched, got %q", after.Email)
	}
	if n := charUsersActiveRefresh(t, u.ID); n != 1 {
		t.Errorf("nome/telefone change must not revoke refresh tokens, active = %d", n)
	}
}

func TestT34_UpdateUser_EmptyBodyIs200NoChange(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "empty@example.com", Nome: "Empty"})
	charUsersCreateRefresh(t, u.ID, "t34-empty-h1", nil)

	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, map[string]interface{}{}, charUsersBearer(), "")
	if got.code != http.StatusOK || got.body["message"] != "user updated" {
		t.Fatalf("expected 200 user updated for empty patch, got %d %s", got.code, got.raw)
	}
	if after := charUsersReload(t, u.ID); after.Nome != "Empty" || after.Email != "empty@example.com" {
		t.Errorf("empty patch changed the user: %+v", after)
	}
	if n := charUsersActiveRefresh(t, u.ID); n != 1 {
		t.Errorf("empty patch must not revoke, active = %d", n)
	}
}

func TestT34_UpdateUser_404UnknownID(t *testing.T) {
	r := charUsersSetup(t)
	got := charUsersDo(t, r, "PATCH", "/v1/users/"+uuid.New().String(),
		map[string]interface{}{"nome": "X"}, charUsersBearer(), "")
	charUsersExpectError(t, got, http.StatusNotFound, "user not found")
}

func TestT34_UpdateUser_400InvalidJSON(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "badjson@example.com", Nome: "Bad"})
	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, `{"nome":`, charUsersBearer(), "")
	charUsersExpectError(t, got, http.StatusBadRequest, "invalid request")
	if _, has := got.body["details"]; has {
		t.Errorf("PATCH invalid JSON currently returns no details, got %s", got.raw)
	}
}

func TestT34_UpdateUser_400InvalidCPFNothingPersisted(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "cpf@example.com", Nome: "Cpf", CPF: charUsersStrPtr("12345678909")})

	for _, bad := range []string{"123", "123.456.789-0", "abcdefghijk", "123456789012", "123-456-789.09"} {
		got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID,
			map[string]interface{}{"nome": "Changed", "cpf": bad}, charUsersBearer(), "")
		if got.code != http.StatusBadRequest || got.body["error"] != "invalid request" || got.body["details"] != "cpf invalid" {
			t.Fatalf("cpf %q: expected 400 invalid request/cpf invalid, got %d %s", bad, got.code, got.raw)
		}
	}
	after := charUsersReload(t, u.ID)
	if after.Nome != "Cpf" || after.CPF == nil || *after.CPF != "12345678909" {
		t.Errorf("rejected patch must persist nothing, got nome=%q cpf=%v", after.Nome, after.CPF)
	}
}

func TestT34_UpdateUser_ValidAndEmptyCPFAccepted(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "cpfok@example.com", Nome: "CpfOk"})

	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, map[string]interface{}{"cpf": "987.654.321-00"}, charUsersBearer(), "")
	if got.code != http.StatusOK {
		t.Fatalf("masked cpf: expected 200, got %d %s", got.code, got.raw)
	}
	if after := charUsersReload(t, u.ID); after.CPF == nil || *after.CPF != "987.654.321-00" {
		t.Errorf("cpf = %v, want 987.654.321-00", after.CPF)
	}
	// Current behavior: "" passes validation and is stored as "" (not NULL).
	got = charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, map[string]interface{}{"cpf": ""}, charUsersBearer(), "")
	if got.code != http.StatusOK {
		t.Fatalf("empty cpf: expected 200, got %d %s", got.code, got.raw)
	}
	if after := charUsersReload(t, u.ID); after.CPF == nil || *after.CPF != "" {
		t.Errorf("empty cpf currently stored as \"\", got %v", after.CPF)
	}
}

func TestT34_UpdateUser_409DuplicateEmailNoChangeNoRevocation(t *testing.T) {
	r := charUsersSetup(t)
	charUsersCreateUser(t, &models.User{Email: "taken@example.com", Nome: "Owner"})
	u := charUsersCreateUser(t, &models.User{Email: "mine@example.com", Nome: "Mine"})
	charUsersCreateRefresh(t, u.ID, "t34-dup-h1", nil)

	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID,
		map[string]interface{}{"email": "taken@example.com", "nome": "Renamed"}, charUsersBearer(), "")
	charUsersExpectError(t, got, http.StatusConflict, "email already exists")

	after := charUsersReload(t, u.ID)
	if after.Email != "mine@example.com" || after.Nome != "Mine" {
		t.Errorf("409 must persist nothing, got email=%q nome=%q", after.Email, after.Nome)
	}
	if n := charUsersActiveRefresh(t, u.ID); n != 1 {
		t.Errorf("409 must not revoke, active = %d", n)
	}
}

func TestT34_UpdateUser_SameEmailIsNoChangeNoRevocation(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "same@example.com", Nome: "Same"})
	charUsersCreateRefresh(t, u.ID, "t34-same-h1", nil)

	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, map[string]interface{}{"email": "same@example.com"}, charUsersBearer(), "")
	if got.code != http.StatusOK {
		t.Fatalf("same email: expected 200, got %d %s", got.code, got.raw)
	}
	if n := charUsersActiveRefresh(t, u.ID); n != 1 {
		t.Errorf("patching the current email is not a change and must not revoke, active = %d", n)
	}
}

func TestT34_UpdateUser_EmailChangeRevokesAllActiveRefreshTokens(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "old-t34@example.com", Nome: "Old"})
	other := charUsersCreateUser(t, &models.User{Email: "other-t34@example.com", Nome: "Other"})
	a := charUsersCreateRefresh(t, u.ID, "t34-email-a", nil)
	b := charUsersCreateRefresh(t, u.ID, "t34-email-b", nil)
	earlier := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	old := charUsersCreateRefresh(t, u.ID, "t34-email-old", &earlier)
	foreign := charUsersCreateRefresh(t, other.ID, "t34-email-foreign", nil)

	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, map[string]interface{}{"email": "new-t34@example.com"}, charUsersBearer(), "")
	if got.code != http.StatusOK || got.body["message"] != "user updated" {
		t.Fatalf("email change: expected 200, got %d %s", got.code, got.raw)
	}
	if after := charUsersReload(t, u.ID); after.Email != "new-t34@example.com" {
		t.Errorf("email = %q, want new-t34@example.com", after.Email)
	}
	for _, rt := range []*models.RefreshToken{a, b} {
		var cur models.RefreshToken
		db.DB().First(&cur, "id = ?", rt.ID)
		if cur.RevokedAt == nil {
			t.Errorf("refresh token %s must be revoked after email change", rt.TokenHash)
		}
	}
	var curOld models.RefreshToken
	db.DB().First(&curOld, "id = ?", old.ID)
	if curOld.RevokedAt == nil || !curOld.RevokedAt.Equal(earlier) {
		t.Errorf("already-revoked token must keep its original revoked_at %v, got %v", earlier, curOld.RevokedAt)
	}
	var curForeign models.RefreshToken
	db.DB().First(&curForeign, "id = ?", foreign.ID)
	if curForeign.RevokedAt != nil {
		t.Error("another user's refresh token must not be revoked")
	}
}

func TestT34_UpdateUser_EmailNotFormatValidated(t *testing.T) {
	// Current behavior (recorded, not fixed): PATCH does not validate email format.
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "fmt@example.com", Nome: "Fmt"})
	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, map[string]interface{}{"email": "not-an-email"}, charUsersBearer(), "")
	if got.code != http.StatusOK {
		t.Fatalf("expected current 200 for unvalidated email, got %d %s", got.code, got.raw)
	}
	if after := charUsersReload(t, u.ID); after.Email != "not-an-email" {
		t.Errorf("email = %q, want not-an-email", after.Email)
	}
}

func TestT34_UpdateUser_EmptyDisabledAtReenablesWithoutRevocation(t *testing.T) {
	r := charUsersSetup(t)
	now := time.Now()
	u := charUsersCreateUser(t, &models.User{Email: "reen@example.com", Nome: "Reen", DisabledAt: &now})
	charUsersCreateRefresh(t, u.ID, "t34-reen-h1", nil)

	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, map[string]interface{}{"disabled_at": ""}, charUsersBearer(), "")
	if got.code != http.StatusOK {
		t.Fatalf("re-enable: expected 200, got %d %s", got.code, got.raw)
	}
	if after := charUsersReload(t, u.ID); after.DisabledAt != nil {
		t.Errorf("disabled_at \"\" must clear it, got %v", after.DisabledAt)
	}
	if n := charUsersActiveRefresh(t, u.ID); n != 1 {
		t.Errorf("re-enable must not revoke, active = %d", n)
	}
}

func TestT34_UpdateUser_AnyNonEmptyDisabledAtDisablesNow(t *testing.T) {
	// Current behavior: the value is ignored; any non-empty string means "now".
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "dis@example.com", Nome: "Dis"})
	before := time.Now().Add(-time.Second)

	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, map[string]interface{}{"disabled_at": "2000-01-01T00:00:00Z"}, charUsersBearer(), "")
	if got.code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d %s", got.code, got.raw)
	}
	after := charUsersReload(t, u.ID)
	if after.DisabledAt == nil || after.DisabledAt.Before(before) {
		t.Errorf("disabled_at must be ~now (value ignored), got %v", after.DisabledAt)
	}
}

func TestT34_UpdateUser_401WithoutServiceKey(t *testing.T) {
	r := charUsersSetup(t)
	u := charUsersCreateUser(t, &models.User{Email: "patchnokey@example.com", Nome: "NoKey"})
	got := charUsersDo(t, r, "PATCH", "/v1/users/"+u.ID, map[string]interface{}{"nome": "Hacked"}, "", "")
	charUsersExpectError(t, got, http.StatusUnauthorized, "missing authorization header")
	if after := charUsersReload(t, u.ID); after.Nome != "NoKey" {
		t.Errorf("unauthenticated patch changed nome to %q", after.Nome)
	}
}

// ------------------------------------------------------------- CreateUser

func TestT34_CreateUser_201ResponseShape(t *testing.T) {
	r := charUsersSetup(t)
	got := charUsersDo(t, r, "POST", "/v1/users",
		map[string]interface{}{"email": "new-user@example.com", "nome": "  New User  ", "cpf": "12345678909"}, charUsersBearer(), "")
	if got.code != http.StatusCreated {
		t.Fatalf("expected 201, got %d %s", got.code, got.raw)
	}
	if got.body["email"] != "new-user@example.com" || got.body["nome"] != "New User" {
		t.Errorf("unexpected body %s", got.raw)
	}
	id, _ := got.body["id"].(string)
	if _, err := uuid.Parse(id); err != nil {
		t.Errorf("id must be a uuid, got %#v", got.body["id"])
	}
	if s, ok := got.body["created_at"].(string); !ok || s == "" {
		t.Errorf("created_at missing: %s", got.raw)
	}
	if len(got.body) != 4 {
		t.Errorf("expected exactly id/email/nome/created_at, got %s", got.raw)
	}
	stored := charUsersReload(t, id)
	if stored.PasswordHash != nil {
		t.Error("user created without password must have no password_hash")
	}
	if stored.CPF == nil || *stored.CPF != "12345678909" {
		t.Errorf("cpf = %v, want 12345678909", stored.CPF)
	}
}

func TestT34_CreateUser_409DuplicateEmail(t *testing.T) {
	r := charUsersSetup(t)
	charUsersCreateUser(t, &models.User{Email: "dup-create@example.com", Nome: "First"})
	got := charUsersDo(t, r, "POST", "/v1/users",
		map[string]interface{}{"email": "dup-create@example.com", "nome": "Second"}, charUsersBearer(), "")
	charUsersExpectError(t, got, http.StatusConflict, "email already exists")
	var n int64
	db.DB().Model(&models.User{}).Where("email = ?", "dup-create@example.com").Count(&n)
	if n != 1 {
		t.Errorf("duplicate create must not insert, count = %d", n)
	}
}

func TestT34_CreateUser_400Validation(t *testing.T) {
	r := charUsersSetup(t)
	cases := []struct {
		name    string
		body    interface{}
		errMsg  string
		details string // "" = not asserted (binding message)
	}{
		{"missing email", map[string]interface{}{"nome": "X"}, "invalid request", ""},
		{"bad email", map[string]interface{}{"email": "nope", "nome": "X"}, "invalid request", ""},
		{"blank nome", map[string]interface{}{"email": "b@example.com", "nome": "   "}, "invalid request", "nome must not be blank"},
		{"bad cpf", map[string]interface{}{"email": "c@example.com", "nome": "X", "cpf": "123"}, "invalid request", "cpf invalid"},
		{"short password", map[string]interface{}{"email": "p@example.com", "nome": "X", "password": "short"}, "password must be at least 8 characters", ""},
	}
	for _, tc := range cases {
		got := charUsersDo(t, r, "POST", "/v1/users", tc.body, charUsersBearer(), "")
		if got.code != http.StatusBadRequest || got.body["error"] != tc.errMsg {
			t.Errorf("%s: expected 400 {error:%q}, got %d %s", tc.name, tc.errMsg, got.code, got.raw)
			continue
		}
		if tc.details != "" && got.body["details"] != tc.details {
			t.Errorf("%s: details = %#v, want %q", tc.name, got.body["details"], tc.details)
		}
	}
	var n int64
	db.DB().Model(&models.User{}).Count(&n)
	if n != 0 {
		t.Errorf("rejected creates must insert nothing, count = %d", n)
	}
}

func TestT34_CreateUser_401WithoutServiceKey(t *testing.T) {
	r := charUsersSetup(t)
	got := charUsersDo(t, r, "POST", "/v1/users", map[string]interface{}{"email": "x@example.com", "nome": "X"}, "", "")
	charUsersExpectError(t, got, http.StatusUnauthorized, "missing authorization header")
}

// ----------------------------------------------------------- CreateInvite

func charUsersInvite(email string) map[string]interface{} {
	return map[string]interface{}{"email": email, "subject": "Invite", "body": "Link {{link}}"}
}

func TestT34_CreateInvite_201MailerStubReturnsToken(t *testing.T) {
	r := charUsersSetup(t)
	got := charUsersDo(t, r, "POST", "/v1/invites", charUsersInvite("inv@example.com"), charUsersBearer(), "")
	if got.code != http.StatusCreated || got.body["message"] != "invite sent" || got.body["email"] != "inv@example.com" {
		t.Fatalf("expected 201 invite sent, got %d %s", got.code, got.raw)
	}
	token, _ := got.body["token"].(string)
	if token == "" {
		t.Fatalf("mailer stub must return the token, got %s", got.raw)
	}
	if s, ok := got.body["expires_at"].(string); !ok || s == "" {
		t.Errorf("mailer stub must return expires_at, got %s", got.raw)
	}
	var inv models.Invite
	if err := db.DB().Where("token_hash = ?", models.HashToken(token)).First(&inv).Error; err != nil {
		t.Fatalf("invite stored by hash of the returned token: %v", err)
	}
	if inv.TokenHash == token {
		t.Error("raw token must not be stored")
	}
	if inv.UsedAt != nil || !inv.ExpiresAt.After(time.Now()) {
		t.Errorf("new invite must be unused and in the future, got used=%v exp=%v", inv.UsedAt, inv.ExpiresAt)
	}
}

func TestT34_CreateInvite_RealMailerOmitsToken(t *testing.T) {
	r := charUsersSetup(t)
	config.Get().MailerStub = false // restored by charUsersSetup cleanup
	got := charUsersDo(t, r, "POST", "/v1/invites", charUsersInvite("inv2@example.com"), charUsersBearer(), "")
	if got.code != http.StatusCreated {
		t.Fatalf("expected 201, got %d %s", got.code, got.raw)
	}
	if _, has := got.body["token"]; has {
		t.Errorf("token must not be returned with a real mailer: %s", got.raw)
	}
	if _, has := got.body["expires_at"]; has {
		t.Errorf("expires_at not returned with a real mailer: %s", got.raw)
	}
}

func TestT34_CreateInvite_SupersedesPreviousUnusedInvite(t *testing.T) {
	r := charUsersSetup(t)
	first := charUsersDo(t, r, "POST", "/v1/invites", charUsersInvite("again@example.com"), charUsersBearer(), "")
	second := charUsersDo(t, r, "POST", "/v1/invites", charUsersInvite("again@example.com"), charUsersBearer(), "")
	if first.code != http.StatusCreated || second.code != http.StatusCreated {
		t.Fatalf("expected 201 twice, got %d / %d", first.code, second.code)
	}
	var old, cur models.Invite
	db.DB().Where("token_hash = ?", models.HashToken(first.body["token"].(string))).First(&old)
	db.DB().Where("token_hash = ?", models.HashToken(second.body["token"].(string))).First(&cur)
	if old.UsedAt == nil {
		t.Error("previous unused invite must be marked used when a new one is created")
	}
	if cur.UsedAt != nil {
		t.Error("new invite must be unused")
	}
}

func TestT34_CreateInvite_401And400(t *testing.T) {
	r := charUsersSetup(t)
	charUsersExpectError(t, charUsersDo(t, r, "POST", "/v1/invites", map[string]interface{}{"email": "a@example.com"}, "", ""),
		http.StatusUnauthorized, "missing authorization header")
	charUsersExpectError(t, charUsersDo(t, r, "POST", "/v1/invites", map[string]interface{}{"email": "a@example.com"}, "Bearer wrong", ""),
		http.StatusUnauthorized, "invalid service key")
	got := charUsersDo(t, r, "POST", "/v1/invites", map[string]interface{}{"email": "nope"}, charUsersBearer(), "")
	charUsersExpectError(t, got, http.StatusBadRequest, "invalid request")
	var n int64
	db.DB().Model(&models.Invite{}).Count(&n)
	if n != 0 {
		t.Errorf("rejected invites must insert nothing, count = %d", n)
	}
}

// ----------------------------------------------------------- AcceptInvite
// Each case uses its own client IP (RemoteAddr) so the global per-IP+path rate limit of
// /v1/invites/accept (5/min) is not shared with handler_test.go.

func TestT34_AcceptInvite_401InvalidToken(t *testing.T) {
	r := charUsersSetup(t)
	got := charUsersDo(t, r, "POST", "/v1/invites/accept",
		map[string]interface{}{"token": "does-not-exist", "password": "password123"}, "", "203.0.113.31:1000")
	charUsersExpectError(t, got, http.StatusUnauthorized, "invalid or expired invite")
	var n int64
	db.DB().Model(&models.User{}).Count(&n)
	if n != 0 {
		t.Errorf("invalid token must create no user, count = %d", n)
	}
}

func TestT34_AcceptInvite_401ExpiredOrUsed(t *testing.T) {
	r := charUsersSetup(t)
	used := time.Now().Add(-time.Minute)
	charUsersCreateInvite(t, &models.Invite{Email: "exp@example.com", TokenHash: models.HashToken("t34-expired"), ExpiresAt: time.Now().Add(-time.Minute)})
	charUsersCreateInvite(t, &models.Invite{Email: "used@example.com", TokenHash: models.HashToken("t34-used"), ExpiresAt: time.Now().Add(time.Hour), UsedAt: &used})

	for _, tok := range []string{"t34-expired", "t34-used"} {
		got := charUsersDo(t, r, "POST", "/v1/invites/accept",
			map[string]interface{}{"token": tok, "password": "password123"}, "", "203.0.113.32:1000")
		charUsersExpectError(t, got, http.StatusUnauthorized, "invalid or expired invite")
	}
}

func TestT34_AcceptInvite_400BadBody(t *testing.T) {
	r := charUsersSetup(t)
	for _, body := range []interface{}{
		map[string]interface{}{"password": "password123"},
		map[string]interface{}{"token": "x", "password": "short"},
		`{"token":`,
	} {
		got := charUsersDo(t, r, "POST", "/v1/invites/accept", body, "", "203.0.113.33:1000")
		if got.code != http.StatusBadRequest || got.body["error"] != "invalid request" {
			t.Errorf("body %v: expected 400 invalid request, got %d %s", body, got.code, got.raw)
		}
		if d, _ := got.body["details"].(string); d == "" {
			t.Errorf("accept 400 currently includes binding details, got %s", got.raw)
		}
	}
}

func TestT34_AcceptInvite_BlankNomeFallsBackToEmailAndInviteConsumed(t *testing.T) {
	r := charUsersSetup(t)
	charUsersCreateInvite(t, &models.Invite{Email: "fallback@example.com", TokenHash: models.HashToken("t34-fallback"), ExpiresAt: time.Now().Add(time.Hour)})

	got := charUsersDo(t, r, "POST", "/v1/invites/accept",
		map[string]interface{}{"token": "t34-fallback", "password": "password123", "nome": "   "}, "", "203.0.113.34:1000")
	if got.code != http.StatusOK || got.body["message"] != "account activated" {
		t.Fatalf("expected 200 account activated, got %d %s", got.code, got.raw)
	}
	id, _ := got.body["user_id"].(string)
	u := charUsersReload(t, id)
	if u.Nome != "fallback@example.com" || u.PasswordHash == nil {
		t.Errorf("blank nome must fall back to the email and set a password, got nome=%q hash=%v", u.Nome, u.PasswordHash != nil)
	}
	var inv models.Invite
	db.DB().Where("token_hash = ?", models.HashToken("t34-fallback")).First(&inv)
	if inv.UsedAt == nil {
		t.Error("accepted invite must be marked used")
	}
	again := charUsersDo(t, r, "POST", "/v1/invites/accept",
		map[string]interface{}{"token": "t34-fallback", "password": "password123"}, "", "203.0.113.34:1000")
	charUsersExpectError(t, again, http.StatusUnauthorized, "invalid or expired invite")
}
