package apikeys

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/models"
)

const (
	updateNotFoundBody       = `{"error":"api key not found"}`
	updateOnlyNameBody       = `{"error":"only name is editable"}`
	updateInvalidRequestBody = `{"error":"invalid request"}`
)

type updateAPIKeyResponse struct {
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

// patchUpdateAPIKey sends PATCH /v1/api-keys/:id; authHeader "" omits the header.
func patchUpdateAPIKey(t *testing.T, id, body, authHeader string) (*httptest.ResponseRecorder, updateAPIKeyResponse) {
	t.Helper()
	router := setupTestRouter()
	req := httptest.NewRequest(http.MethodPatch, "/v1/api-keys/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp updateAPIKeyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Logf("response body is not JSON (status %d): %s", w.Code, w.Body.String())
	}
	return w, resp
}

// updateJSON marshals fields into a request body.
func updateJSON(t *testing.T, fields map[string]any) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(b)
}

// insertUpdateTestKey stores a key (hash only) named name, with optional expiry and revocation.
func insertUpdateTestKey(t *testing.T, ownerID, audience string, name *string, expiresAt, revokedAt *time.Time) *models.APIKey {
	t.Helper()
	key := &models.APIKey{
		OwnerUserID:  ownerID,
		Audience:     audience,
		AudienceType: "text",
		Name:         name,
		KeyHash:      models.HashToken("jns_" + models.GenerateOpaqueToken()),
		ExpiresAt:    expiresAt,
		RevokedAt:    revokedAt,
	}
	if err := db.DB().Create(key).Error; err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	return key
}

func reloadUpdateTestKey(t *testing.T, id string) models.APIKey {
	t.Helper()
	var stored models.APIKey
	if err := db.DB().First(&stored, "id = ?", id).Error; err != nil {
		t.Fatalf("reload api key %s: %v", id, err)
	}
	return stored
}

func updateStrPtr(v string) *string { return &v }

func updateTimePtr(v time.Time) *time.Time { return &v }

func assertUpdateError(t *testing.T, w *httptest.ResponseRecorder, status int, body, label string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("%s: expected %d, got %d: %s", label, status, w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != body {
		t.Fatalf("%s: expected body %s, got %s", label, body, w.Body.String())
	}
}

// assertKeyUnchanged compares every persisted column of the reloaded key with original.
func assertKeyUnchanged(t *testing.T, original *models.APIKey, label string) {
	t.Helper()
	stored := reloadUpdateTestKey(t, original.ID)
	if (stored.Name == nil) != (original.Name == nil) || (stored.Name != nil && *stored.Name != *original.Name) {
		t.Fatalf("%s: name changed from %v to %v", label, original.Name, stored.Name)
	}
	if stored.KeyHash != original.KeyHash {
		t.Fatalf("%s: key_hash changed", label)
	}
	if stored.OwnerUserID != original.OwnerUserID || stored.Audience != original.Audience || stored.AudienceType != original.AudienceType {
		t.Fatalf("%s: owner/audience/audience_type changed: %+v", label, stored)
	}
	if (stored.ExpiresAt == nil) != (original.ExpiresAt == nil) || (stored.RevokedAt == nil) != (original.RevokedAt == nil) {
		t.Fatalf("%s: expires_at/revoked_at changed: %v %v", label, stored.ExpiresAt, stored.RevokedAt)
	}
}

func TestUpdateAPIKeyRenameReturns200AndKeepsOtherFields(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("old"), nil, nil)

	w, resp := patchUpdateAPIKey(t, key.ID, `{"name":"ERP"}`, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.Name == nil || *resp.APIKey.Name != "ERP" {
		t.Fatalf("expected name ERP in response, got %v", resp.APIKey.Name)
	}
	if resp.APIKey.ID != key.ID || resp.APIKey.OwnerUserID != owner.ID || resp.APIKey.Audience != "crazy" || resp.APIKey.AudienceType != "text" {
		t.Fatalf("unexpected projection: %+v", resp.APIKey)
	}
	if resp.APIKey.RevokedAt != nil || resp.APIKey.ExpiresAt != nil || resp.APIKey.CreatedAt.IsZero() {
		t.Fatalf("unexpected timestamps: %+v", resp.APIKey)
	}
	raw := w.Body.String()
	if strings.Contains(raw, "key_hash") || strings.Contains(raw, key.KeyHash) {
		t.Fatalf("response must not expose key_hash: %s", raw)
	}
	if strings.Contains(raw, `"key"`) {
		t.Fatalf("response must not contain a raw key field: %s", raw)
	}

	stored := reloadUpdateTestKey(t, key.ID)
	if stored.Name == nil || *stored.Name != "ERP" {
		t.Fatalf("expected stored name ERP, got %v", stored.Name)
	}
	if stored.KeyHash != key.KeyHash {
		t.Fatal("key_hash must stay unchanged after rename")
	}
}

func TestUpdateAPIKeyWithMatchingAudienceRenames(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertUpdateTestKey(t, owner.ID, "crazy", nil, nil, nil)

	w, resp := patchUpdateAPIKey(t, key.ID, `{"name":"ERP","audience":"crazy"}`, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.Name == nil || *resp.APIKey.Name != "ERP" {
		t.Fatalf("expected name ERP, got %v", resp.APIKey.Name)
	}
	if stored := reloadUpdateTestKey(t, key.ID); stored.Name == nil || *stored.Name != "ERP" {
		t.Fatalf("expected stored name ERP, got %v", stored.Name)
	}
}

func TestUpdateAPIKeyEmptyBlankOrNullNameStoresNull(t *testing.T) {
	cases := map[string]string{
		"empty":  `{"name":""}`,
		"blank":  `{"name":"   "}`,
		"null":   `{"name":null}`,
		"tabsnl": `{"name":"\t\n "}`,
	}
	for label, body := range cases {
		t.Run(label, func(t *testing.T) {
			setupTestDB(t)
			owner := createTestUser(t, "owner@example.com", false)
			key := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("old"), nil, nil)

			w, resp := patchUpdateAPIKey(t, key.ID, body, "Bearer "+TEST_SERVICE_KEY)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			if resp.APIKey.Name != nil {
				t.Fatalf("expected name null in response, got %q", *resp.APIKey.Name)
			}
			if !strings.Contains(w.Body.String(), `"name":null`) {
				t.Fatalf("expected raw JSON \"name\":null, got %s", w.Body.String())
			}
			if stored := reloadUpdateTestKey(t, key.ID); stored.Name != nil {
				t.Fatalf("expected stored name NULL, got %q", *stored.Name)
			}
		})
	}
}

func TestUpdateAPIKeyNameLengthBoundaries(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("old"), nil, nil)

	name100 := strings.Repeat("a", 100)
	w, resp := patchUpdateAPIKey(t, key.ID, updateJSON(t, map[string]any{"name": name100}), "Bearer "+TEST_SERVICE_KEY)
	if w.Code != http.StatusOK {
		t.Fatalf("100 chars: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.Name == nil || *resp.APIKey.Name != name100 {
		t.Fatalf("100 chars: expected name echoed, got %v", resp.APIKey.Name)
	}

	renamed := reloadUpdateTestKey(t, key.ID)
	w, _ = patchUpdateAPIKey(t, key.ID, updateJSON(t, map[string]any{"name": strings.Repeat("b", 101)}), "Bearer "+TEST_SERVICE_KEY)
	assertUpdateError(t, w, http.StatusBadRequest, updateInvalidRequestBody, "101 chars")
	assertKeyUnchanged(t, &renamed, "101 chars")
}

func TestUpdateAPIKeyNonNameFieldReturns400OnlyNameIsEditable(t *testing.T) {
	cases := map[string]map[string]any{
		"expires_in_days":         {"expires_in_days": 10},
		"expires_in_days_null":    {"expires_in_days": nil},
		"audience_type":           {"audience_type": "x"},
		"owner_user_id":           {"owner_user_id": uuid.New().String()},
		"key":                     {"key": "jns_" + strings.Repeat("0", 64)},
		"expires_at":              {"expires_at": "2030-01-01T00:00:00Z"},
		"revoked_at":              {"revoked_at": nil},
		"name_plus_expires":       {"name": "ERP", "expires_in_days": 10},
		"name_plus_audience_type": {"name": "ERP", "audience_type": "text"},
	}
	for label, fields := range cases {
		t.Run(label, func(t *testing.T) {
			setupTestDB(t)
			owner := createTestUser(t, "owner@example.com", false)
			key := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("old"), nil, nil)

			w, _ := patchUpdateAPIKey(t, key.ID, updateJSON(t, fields), "Bearer "+TEST_SERVICE_KEY)

			assertUpdateError(t, w, http.StatusBadRequest, updateOnlyNameBody, label)
			assertKeyUnchanged(t, key, label)
		})
	}
}

func TestUpdateAPIKeyMalformedBodyReturns400InvalidRequest(t *testing.T) {
	cases := map[string]string{
		"malformed json": `{"name":`,
		"name not str":   `{"name":123}`,
	}
	for label, body := range cases {
		t.Run(label, func(t *testing.T) {
			setupTestDB(t)
			owner := createTestUser(t, "owner@example.com", false)
			key := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("old"), nil, nil)

			w, _ := patchUpdateAPIKey(t, key.ID, body, "Bearer "+TEST_SERVICE_KEY)

			assertUpdateError(t, w, http.StatusBadRequest, updateInvalidRequestBody, label)
			assertKeyUnchanged(t, key, label)
		})
	}
}

func TestUpdateAPIKeyDivergentAudienceReturns404WithoutEffect(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("old"), nil, nil)

	w, _ := patchUpdateAPIKey(t, key.ID, `{"name":"hijack","audience":"outro"}`, "Bearer "+TEST_SERVICE_KEY)

	assertUpdateError(t, w, http.StatusNotFound, updateNotFoundBody, "divergent audience")
	assertKeyUnchanged(t, key, "divergent audience")
}

func TestUpdateAPIKeyUnknownOrNonUUIDIDReturns404(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("old"), nil, nil)

	for _, id := range []string{uuid.New().String(), "not-a-uuid", "1%20OR%201%3D1"} {
		w, _ := patchUpdateAPIKey(t, id, `{"name":"ERP"}`, "Bearer "+TEST_SERVICE_KEY)
		assertUpdateError(t, w, http.StatusNotFound, updateNotFoundBody, "id="+id)
	}
	assertKeyUnchanged(t, key, "after unknown ids")
}

func TestUpdateAPIKeyRevokedOrExpiredKeyCanBeRenamed(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	revokedAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	expiredAt := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	revoked := insertUpdateTestKey(t, owner.ID, "crazy", nil, nil, updateTimePtr(revokedAt))
	expired := insertUpdateTestKey(t, owner.ID, "crazy", nil, updateTimePtr(expiredAt), nil)

	w, resp := patchUpdateAPIKey(t, revoked.ID, `{"name":"old ERP"}`, "Bearer "+TEST_SERVICE_KEY)
	if w.Code != http.StatusOK {
		t.Fatalf("revoked: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.RevokedAt == nil || !resp.APIKey.RevokedAt.Equal(revokedAt) {
		t.Fatalf("revoked: revoked_at must be kept, got %v", resp.APIKey.RevokedAt)
	}
	if stored := reloadUpdateTestKey(t, revoked.ID); stored.RevokedAt == nil || stored.Name == nil || *stored.Name != "old ERP" {
		t.Fatalf("revoked: expected renamed and still revoked, got %+v", stored)
	}

	w, resp = patchUpdateAPIKey(t, expired.ID, `{"name":"expired ERP"}`, "Bearer "+TEST_SERVICE_KEY)
	if w.Code != http.StatusOK {
		t.Fatalf("expired: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.ExpiresAt == nil || !resp.APIKey.ExpiresAt.Equal(expiredAt) {
		t.Fatalf("expired: expires_at must be kept, got %v", resp.APIKey.ExpiresAt)
	}
}

func TestUpdateAPIKeySameNameTwiceIsIdempotent(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertUpdateTestKey(t, owner.ID, "crazy", nil, nil, nil)

	w1, _ := patchUpdateAPIKey(t, key.ID, `{"name":"ERP"}`, "Bearer "+TEST_SERVICE_KEY)
	w2, _ := patchUpdateAPIKey(t, key.ID, `{"name":"ERP"}`, "Bearer "+TEST_SERVICE_KEY)

	if w1.Code != http.StatusOK || w2.Code != http.StatusOK {
		t.Fatalf("expected 200 twice, got %d and %d: %s", w1.Code, w2.Code, w2.Body.String())
	}
	if w1.Body.String() != w2.Body.String() {
		t.Fatalf("same name must give the same result:\n%s\n%s", w1.Body.String(), w2.Body.String())
	}
}

func TestUpdateAPIKeyOnlyAffectsTargetKey(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	target := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("a"), nil, nil)
	other := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("b"), nil, nil)

	w, _ := patchUpdateAPIKey(t, target.ID, `{"name":"ERP"}`, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertKeyUnchanged(t, other, "other key")
}

func TestUpdateAPIKeyNameIsStoredLiterally(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertUpdateTestKey(t, owner.ID, "crazy", nil, nil, nil)
	name := `x'; DROP TABLE api_keys; -- <script>`

	w, resp := patchUpdateAPIKey(t, key.ID, updateJSON(t, map[string]any{"name": name}), "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.Name == nil || *resp.APIKey.Name != name {
		t.Fatalf("expected name stored literally, got %v", resp.APIKey.Name)
	}
	if stored := reloadUpdateTestKey(t, key.ID); stored.Name == nil || *stored.Name != name {
		t.Fatalf("expected stored name literal, got %v", stored.Name)
	}
}

func TestUpdateAPIKeyWithoutServiceKeyReturns401WithoutEffect(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	key := insertUpdateTestKey(t, owner.ID, "crazy", updateStrPtr("old"), nil, nil)

	for _, h := range []string{"", "Bearer wrong-key", TEST_SERVICE_KEY} {
		w, _ := patchUpdateAPIKey(t, key.ID, `{"name":"hijack"}`, h)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("auth=%q: expected 401, got %d: %s", h, w.Code, w.Body.String())
		}
	}
	assertKeyUnchanged(t, key, "after unauthorized updates")
}
