package apikeys

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
)

// verifyInvalidAPIKeyBody is the single 401 body for every invalid key reason (04-contracts).
const verifyInvalidAPIKeyBody = `{"error":"invalid api key"}`

const verifyInvalidRequestBody = `{"error":"invalid request"}`

// postVerifyAPIKey sends POST /v1/api-keys/verify with a raw body; authHeader "" omits the header.
func postVerifyAPIKey(t *testing.T, body, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	router := setupTestRouter()
	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// verifyBody encodes {key, audience} as JSON.
func verifyBody(t *testing.T, key, audience string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"key": key, "audience": audience})
	if err != nil {
		t.Fatalf("marshal verify body: %v", err)
	}
	return string(raw)
}

// insertVerifyTestKey stores rawKey (hash only) for owner/audience with optional expires_at and revoked_at.
func insertVerifyTestKey(t *testing.T, ownerID, audience, rawKey string, expiresAt, revokedAt *time.Time) *models.APIKey {
	t.Helper()
	key := &models.APIKey{
		OwnerUserID:  ownerID,
		Audience:     audience,
		AudienceType: "text",
		KeyHash:      models.HashToken(rawKey),
		ExpiresAt:    expiresAt,
		RevokedAt:    revokedAt,
	}
	if err := db.DB().Create(key).Error; err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	return key
}

func newVerifyRawKey() string {
	return "jns_" + models.GenerateOpaqueToken()
}

func verifyTimePtr(v time.Time) *time.Time {
	return &v
}

func TestVerifyAPIKeyValidKeyReturns200WithOwnerAndAudience(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	rawKey := newVerifyRawKey()
	key := insertVerifyTestKey(t, owner.ID, "crazy", rawKey, nil, nil)

	w := postVerifyAPIKey(t, verifyBody(t, rawKey, "crazy"), "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v: %s", err, w.Body.String())
	}
	if resp["valid"] != true {
		t.Fatalf("expected valid=true, got %v", resp["valid"])
	}
	if resp["key_id"] != key.ID {
		t.Fatalf("expected key_id %q, got %v", key.ID, resp["key_id"])
	}
	if resp["owner_user_id"] != owner.ID {
		t.Fatalf("expected owner_user_id %q, got %v", owner.ID, resp["owner_user_id"])
	}
	if resp["audience"] != "crazy" {
		t.Fatalf("expected audience crazy, got %v", resp["audience"])
	}
	fields := make([]string, 0, len(resp))
	for name := range resp {
		fields = append(fields, name)
	}
	sort.Strings(fields)
	if strings.Join(fields, ",") != "audience,key_id,owner_user_id,valid" {
		t.Fatalf("expected exactly audience,key_id,owner_user_id,valid (no scopes/roles/secrets), got %v", fields)
	}
	if strings.Contains(w.Body.String(), rawKey) || strings.Contains(w.Body.String(), key.KeyHash) {
		t.Fatalf("verify response leaks the raw key or its hash: %s", w.Body.String())
	}
}

func TestVerifyAPIKeyNotYetExpiredKeyReturns200(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	rawKey := newVerifyRawKey()
	insertVerifyTestKey(t, owner.ID, "crazy", rawKey, verifyTimePtr(time.Now().Add(time.Hour)), nil)

	w := postVerifyAPIKey(t, verifyBody(t, rawKey, "crazy"), "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for a key expiring in 1h, got %d: %s", w.Code, w.Body.String())
	}
}

func TestVerifyAPIKeyEveryInvalidReasonReturnsIdentical401(t *testing.T) {
	setupTestDB(t)
	active := createTestUser(t, "active@example.com", false)
	disabled := createTestUser(t, "disabled@example.com", true)

	revokedKey := newVerifyRawKey()
	insertVerifyTestKey(t, active.ID, "crazy", revokedKey, nil, verifyTimePtr(time.Now().Add(-time.Minute)))

	expiredKey := newVerifyRawKey()
	insertVerifyTestKey(t, active.ID, "crazy", expiredKey, verifyTimePtr(time.Now().Add(-time.Second)), nil)

	disabledOwnerKey := newVerifyRawKey()
	insertVerifyTestKey(t, disabled.ID, "crazy", disabledOwnerKey, nil, nil)

	otherAudienceKey := newVerifyRawKey()
	insertVerifyTestKey(t, active.ID, "outro", otherAudienceKey, nil, nil)

	// Stored hash matches exactly, but the key lacks the jns_ prefix: the prefix check must reject it.
	unprefixedKey := models.GenerateOpaqueToken()
	insertVerifyTestKey(t, active.ID, "crazy", unprefixedKey, nil, nil)

	cases := []struct {
		name     string
		key      string
		audience string
	}{
		{"unknown key", newVerifyRawKey(), "crazy"},
		{"bare prefix", "jns_", "crazy"},
		{"revoked", revokedKey, "crazy"},
		{"expired", expiredKey, "crazy"},
		{"owner disabled", disabledOwnerKey, "crazy"},
		{"audience mismatch", otherAudienceKey, "crazy"},
		{"audience differs only by case", otherAudienceKey, "OUTRO"},
		{"no jns_ prefix with matching hash", unprefixedKey, "crazy"},
		{"wrong prefix case", "JNS_" + strings.TrimPrefix(newVerifyRawKey(), "jns_"), "crazy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postVerifyAPIKey(t, verifyBody(t, tc.key, tc.audience), "Bearer "+TEST_SERVICE_KEY)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
			}
			if got := w.Body.String(); got != verifyInvalidAPIKeyBody {
				t.Fatalf("expected body byte-identical to %s, got %s", verifyInvalidAPIKeyBody, got)
			}
		})
	}
}

func TestVerifyAPIKeyInvalidRequestReturns400(t *testing.T) {
	setupTestDB(t)

	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{"key":`},
		{"empty body", ``},
		{"missing key", `{"audience":"crazy"}`},
		{"missing audience", `{"key":"jns_abc"}`},
		{"key not a string", `{"key":123,"audience":"crazy"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := postVerifyAPIKey(t, tc.body, "Bearer "+TEST_SERVICE_KEY)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
			if got := w.Body.String(); got != verifyInvalidRequestBody {
				t.Fatalf("expected %s, got %s", verifyInvalidRequestBody, got)
			}
		})
	}
}

func TestVerifyAPIKeyWithoutServiceKeyReturns401NotInvalidAPIKey(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	rawKey := newVerifyRawKey()
	insertVerifyTestKey(t, owner.ID, "crazy", rawKey, nil, nil)

	for _, header := range []string{"", "Bearer wrong-service-key", TEST_SERVICE_KEY} {
		w := postVerifyAPIKey(t, verifyBody(t, rawKey, "crazy"), header)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("header %q: expected 401, got %d: %s", header, w.Code, w.Body.String())
		}
		// crazy-back maps a service-key 401 to 503 (fail closed); it must not look like "invalid api key".
		if w.Body.String() == verifyInvalidAPIKeyBody {
			t.Fatalf("header %q: service-key failure must not return the invalid api key body", header)
		}
		if strings.Contains(w.Body.String(), "valid") && strings.Contains(w.Body.String(), owner.ID) {
			t.Fatalf("header %q: verify result leaked without service key: %s", header, w.Body.String())
		}
	}
}

func TestVerifyAPIKeyDoesNotWrite(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	rawKey := newVerifyRawKey()
	key := insertVerifyTestKey(t, owner.ID, "crazy", rawKey, nil, nil)
	revokedKey := newVerifyRawKey()
	insertVerifyTestKey(t, owner.ID, "crazy", revokedKey, nil, verifyTimePtr(time.Now().Add(-time.Minute)))

	var before models.APIKey
	if err := db.DB().First(&before, "id = ?", key.ID).Error; err != nil {
		t.Fatalf("load key: %v", err)
	}

	if w := postVerifyAPIKey(t, verifyBody(t, rawKey, "crazy"), "Bearer "+TEST_SERVICE_KEY); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	postVerifyAPIKey(t, verifyBody(t, revokedKey, "crazy"), "Bearer "+TEST_SERVICE_KEY)
	postVerifyAPIKey(t, verifyBody(t, newVerifyRawKey(), "crazy"), "Bearer "+TEST_SERVICE_KEY)

	var after models.APIKey
	if err := db.DB().First(&after, "id = ?", key.ID).Error; err != nil {
		t.Fatalf("reload key: %v", err)
	}
	if after.RevokedAt != nil || after.ExpiresAt != nil || after.KeyHash != before.KeyHash ||
		after.Audience != before.Audience || after.OwnerUserID != before.OwnerUserID {
		t.Fatalf("verify mutated the key: before=%+v after=%+v", before, after)
	}
	var count int64
	db.DB().Model(&models.APIKey{}).Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 rows after verify, got %d", count)
	}
}

func TestVerifyAPIKeyRawKeyNeverInStdout(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	validKey := newVerifyRawKey()
	insertVerifyTestKey(t, owner.ID, "crazy", validKey, nil, nil)
	unknownKey := newVerifyRawKey()
	logger.Init("debug")
	defer logger.Init("info")

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = writer
	captured := make(chan string)
	go func() {
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, reader); err != nil {
			captured <- "stdout capture failed: " + err.Error()
			return
		}
		captured <- buf.String()
	}()

	okResp := postVerifyAPIKey(t, verifyBody(t, validKey, "crazy"), "Bearer "+TEST_SERVICE_KEY)
	badResp := postVerifyAPIKey(t, verifyBody(t, unknownKey, "crazy"), "Bearer "+TEST_SERVICE_KEY)

	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	out := <-captured

	if okResp.Code != http.StatusOK || badResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 200 and 401, got %d and %d", okResp.Code, badResp.Code)
	}
	for _, raw := range []string{validKey, unknownKey} {
		if strings.Contains(out, raw) || strings.Contains(out, strings.TrimPrefix(raw, "jns_")) {
			t.Fatal("raw api key or its secret leaked to stdout during verify")
		}
	}
}
