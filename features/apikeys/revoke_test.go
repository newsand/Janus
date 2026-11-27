package apikeys

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
)

const revokeNotFoundBody = `{"error":"api key not found"}`

type revokeAPIKeyResponse struct {
	Error  string `json:"error"`
	APIKey struct {
		ID           string     `json:"id"`
		OwnerUserID  string     `json:"owner_user_id"`
		Name         *string    `json:"name"`
		Audience     string     `json:"audience"`
		AudienceType string     `json:"audience_type"`
		ExpiresAt    *time.Time `json:"expires_at"`
		RevokedAt    *time.Time `json:"revoked_at"`
		CreatedAt    time.Time  `json:"created_at"`
	} `json:"api_key"`
}

// postRevokeAPIKey sends POST /v1/api-keys/:id/revoke; body "" sends no body, authHeader "" omits the header.
func postRevokeAPIKey(t *testing.T, id, body, authHeader string) (*httptest.ResponseRecorder, revokeAPIKeyResponse) {
	t.Helper()
	router := setupTestRouter()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys/"+id+"/revoke", reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp revokeAPIKeyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Logf("response body is not JSON (status %d): %s", w.Code, w.Body.String())
	}
	return w, resp
}

// insertRevokeTestKey stores a key built from rawKey (hash only) with the given audience and revoked_at.
func insertRevokeTestKey(t *testing.T, ownerID, audience, rawKey string, revokedAt *time.Time) *models.APIKey {
	t.Helper()
	key := &models.APIKey{
		OwnerUserID:  ownerID,
		Audience:     audience,
		AudienceType: "text",
		KeyHash:      models.HashToken(rawKey),
		RevokedAt:    revokedAt,
	}
	if err := db.DB().Create(key).Error; err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	return key
}

func reloadRevokeTestKey(t *testing.T, id string) models.APIKey {
	t.Helper()
	var stored models.APIKey
	if err := db.DB().First(&stored, "id = ?", id).Error; err != nil {
		t.Fatalf("reload api key %s: %v", id, err)
	}
	return stored
}

func assertRevokeNotFound(t *testing.T, w *httptest.ResponseRecorder, label string) {
	t.Helper()
	if w.Code != http.StatusNotFound {
		t.Fatalf("%s: expected 404, got %d: %s", label, w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != revokeNotFoundBody {
		t.Fatalf("%s: expected body %s, got %s", label, revokeNotFoundBody, w.Body.String())
	}
}

func assertKeyStillActive(t *testing.T, id, label string) {
	t.Helper()
	if stored := reloadRevokeTestKey(t, id); stored.RevokedAt != nil {
		t.Fatalf("%s: key must stay active, got revoked_at=%s", label, stored.RevokedAt)
	}
}

func TestRevokeAPIKeySetsRevokedAtAndReturnsProjection(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-happy", nil)

	before := time.Now()
	w, resp := postRevokeAPIKey(t, key.ID, "", "Bearer "+TEST_SERVICE_KEY)
	after := time.Now()

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.ID != key.ID || resp.APIKey.OwnerUserID != owner.ID || resp.APIKey.Audience != "crazy" || resp.APIKey.AudienceType != "text" {
		t.Fatalf("unexpected api_key projection: %+v", resp.APIKey)
	}
	if resp.APIKey.RevokedAt == nil {
		t.Fatalf("expected revoked_at set in response: %s", w.Body.String())
	}
	lo, hi := before.Add(-2*time.Second), after.Add(2*time.Second)
	if resp.APIKey.RevokedAt.Before(lo) || resp.APIKey.RevokedAt.After(hi) {
		t.Fatalf("expected revoked_at ≈ now in [%s, %s], got %s", lo, hi, resp.APIKey.RevokedAt)
	}
	raw := w.Body.String()
	if strings.Contains(raw, "key_hash") || strings.Contains(raw, key.KeyHash) || strings.Contains(raw, "jns_") {
		t.Fatalf("response must not expose key_hash or raw key: %s", raw)
	}

	stored := reloadRevokeTestKey(t, key.ID)
	if stored.RevokedAt == nil {
		t.Fatal("expected revoked_at persisted in the database")
	}
	if !stored.RevokedAt.Equal(*resp.APIKey.RevokedAt) {
		t.Fatalf("persisted revoked_at %s differs from response %s", stored.RevokedAt, resp.APIKey.RevokedAt)
	}
	if stored.KeyHash != key.KeyHash {
		t.Fatal("revoke must not change key_hash")
	}
}

func TestRevokeAPIKeyWithMatchingAudienceRevokes(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-scoped", nil)

	w, resp := postRevokeAPIKey(t, key.ID, `{"audience":"crazy"}`, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.RevokedAt == nil {
		t.Fatalf("expected revoked_at set: %s", w.Body.String())
	}
	if reloadRevokeTestKey(t, key.ID).RevokedAt == nil {
		t.Fatal("expected revoked_at persisted")
	}
}

func TestRevokeAPIKeyEmptyJSONBodyRevokesWithoutScope(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-empty-body", nil)

	w, resp := postRevokeAPIKey(t, key.ID, `{}`, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.RevokedAt == nil {
		t.Fatalf("expected revoked_at set: %s", w.Body.String())
	}
}

func TestRevokeAPIKeyTwiceIsIdempotentAndKeepsOriginalRevokedAt(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-twice", nil)

	w1, first := postRevokeAPIKey(t, key.ID, "", "Bearer "+TEST_SERVICE_KEY)
	if w1.Code != http.StatusOK || first.APIKey.RevokedAt == nil {
		t.Fatalf("first revoke: expected 200 with revoked_at, got %d: %s", w1.Code, w1.Body.String())
	}
	time.Sleep(20 * time.Millisecond)
	w2, second := postRevokeAPIKey(t, key.ID, "", "Bearer "+TEST_SERVICE_KEY)

	if w2.Code != http.StatusOK {
		t.Fatalf("second revoke: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	if second.APIKey.RevokedAt == nil || !second.APIKey.RevokedAt.Equal(*first.APIKey.RevokedAt) {
		t.Fatalf("second revoke must return original revoked_at %s, got %v", first.APIKey.RevokedAt, second.APIKey.RevokedAt)
	}
	if stored := reloadRevokeTestKey(t, key.ID); stored.RevokedAt == nil || !stored.RevokedAt.Equal(*first.APIKey.RevokedAt) {
		t.Fatalf("persisted revoked_at must stay %s, got %v", first.APIKey.RevokedAt, stored.RevokedAt)
	}
}

func TestRevokeAPIKeyAlreadyRevokedKeepsPastRevokedAt(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	past := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	key := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-already", &past)

	w, resp := postRevokeAPIKey(t, key.ID, `{"audience":"crazy"}`, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.RevokedAt == nil || !resp.APIKey.RevokedAt.Equal(past) {
		t.Fatalf("expected original revoked_at %s, got %v", past, resp.APIKey.RevokedAt)
	}
	if stored := reloadRevokeTestKey(t, key.ID); stored.RevokedAt == nil || !stored.RevokedAt.Equal(past) {
		t.Fatalf("persisted revoked_at must stay %s, got %v", past, stored.RevokedAt)
	}
}

func TestRevokeAPIKeyUnknownOrNonUUIDIDReturns404(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-bystander", nil)

	ids := []string{
		uuid.New().String(),
		"abc",
		"123",
		key.ID + "x",
		"'%20OR%20'1'='1",
	}
	for _, id := range ids {
		w, _ := postRevokeAPIKey(t, id, "", "Bearer "+TEST_SERVICE_KEY)
		assertRevokeNotFound(t, w, "id="+id)
	}
	assertKeyStillActive(t, key.ID, "unrelated key after unknown-id revokes")
}

func TestRevokeAPIKeyDivergentAudienceReturns404WithoutEffect(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-divergent", nil)

	bodies := []string{
		`{"audience":"outro"}`,
		`{"audience":"CRAZY"}`,
		`{"audience":"crazy "}`,
		`{"audience":"' OR '1'='1"}`,
		`{"audience":"%"}`,
	}
	for _, body := range bodies {
		w, _ := postRevokeAPIKey(t, key.ID, body, "Bearer "+TEST_SERVICE_KEY)
		assertRevokeNotFound(t, w, "body="+body)
	}
	assertKeyStillActive(t, key.ID, "after divergent-audience revokes")
}

func TestRevokeAPIKeyOnlyAffectsTargetKey(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	target := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-target", nil)
	sameOwner := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-same-owner", nil)
	otherAudience := insertRevokeTestKey(t, owner.ID, "outro", "jns_t06-other-audience", nil)

	w, _ := postRevokeAPIKey(t, target.ID, `{"audience":"crazy"}`, "Bearer "+TEST_SERVICE_KEY)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if reloadRevokeTestKey(t, target.ID).RevokedAt == nil {
		t.Fatal("target key must be revoked")
	}
	assertKeyStillActive(t, sameOwner.ID, "same-owner key")
	assertKeyStillActive(t, otherAudience.ID, "other-audience key")
}

func TestRevokeAPIKeyWithoutServiceKeyReturns401WithoutEffect(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertRevokeTestKey(t, owner.ID, "crazy", "jns_t06-noauth", nil)

	headers := []string{"", "Bearer wrong-service-key", TEST_SERVICE_KEY}
	for _, h := range headers {
		w, _ := postRevokeAPIKey(t, key.ID, "", h)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("auth=%q: expected 401, got %d: %s", h, w.Code, w.Body.String())
		}
	}
	assertKeyStillActive(t, key.ID, "after unauthorized revokes")
}

func TestRevokeAPIKeyRawKeyAndHashNeverInStdout(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	rawKey := "jns_" + models.GenerateOpaqueToken()
	key := insertRevokeTestKey(t, owner.ID, "crazy", rawKey, nil)
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

	w, _ := postRevokeAPIKey(t, key.ID, `{"audience":"crazy"}`, "Bearer "+TEST_SERVICE_KEY)
	wMiss, _ := postRevokeAPIKey(t, key.ID, `{"audience":"outro"}`, "Bearer "+TEST_SERVICE_KEY)

	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	out := <-captured

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if wMiss.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on divergent audience, got %d: %s", wMiss.Code, wMiss.Body.String())
	}
	if strings.Contains(out, strings.TrimPrefix(rawKey, "jns_")) {
		t.Fatal("raw api key secret leaked to stdout during revoke")
	}
	if strings.Contains(out, key.KeyHash) {
		t.Fatal("api key hash leaked to stdout during revoke")
	}
}
