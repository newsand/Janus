package apikeys

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
)

type listAPIKeysResponse struct {
	Error   string `json:"error"`
	APIKeys []struct {
		ID           string     `json:"id"`
		OwnerUserID  string     `json:"owner_user_id"`
		Name         *string    `json:"name"`
		Audience     string     `json:"audience"`
		AudienceType string     `json:"audience_type"`
		ExpiresAt    *time.Time `json:"expires_at"`
		RevokedAt    *time.Time `json:"revoked_at"`
		CreatedAt    time.Time  `json:"created_at"`
	} `json:"api_keys"`
}

// getListAPIKeys sends GET /v1/api-keys with the given query; authHeader "" omits the header.
func getListAPIKeys(t *testing.T, query url.Values, authHeader string) (*httptest.ResponseRecorder, listAPIKeysResponse) {
	t.Helper()
	router := setupTestRouter()
	target := "/v1/api-keys"
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp listAPIKeysResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Logf("response body is not JSON (status %d): %s", w.Code, w.Body.String())
	}
	return w, resp
}

// insertListTestKey stores an api key row directly with a fixed created_at; id "" lets BeforeCreate assign one.
func insertListTestKey(t *testing.T, id, ownerID, audience string, createdAt time.Time) *models.APIKey {
	t.Helper()
	key := &models.APIKey{
		ID:           id,
		OwnerUserID:  ownerID,
		Audience:     audience,
		AudienceType: "text",
		KeyHash:      models.HashToken("jns_" + models.GenerateOpaqueToken()),
		CreatedAt:    createdAt,
	}
	if err := db.DB().Create(key).Error; err != nil {
		t.Fatalf("insert api key: %v", err)
	}
	return key
}

func listedIDs(resp listAPIKeysResponse) []string {
	ids := make([]string, 0, len(resp.APIKeys))
	for _, k := range resp.APIKeys {
		ids = append(ids, k.ID)
	}
	return ids
}

func assertListedIDs(t *testing.T, resp listAPIKeysResponse, want ...string) {
	t.Helper()
	got := listedIDs(resp)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected ids %v (in this order), got %v", want, got)
	}
}

func assertNoSecretsInListJSON(t *testing.T, raw string, keys ...*models.APIKey) {
	t.Helper()
	if strings.Contains(raw, "key_hash") {
		t.Fatalf("list JSON must not contain key_hash: %s", raw)
	}
	if strings.Contains(raw, `"key"`) {
		t.Fatalf("list JSON must not contain a raw key field: %s", raw)
	}
	if strings.Contains(raw, "jns_") {
		t.Fatalf("list JSON must not contain a raw jns_ key: %s", raw)
	}
	for _, k := range keys {
		if strings.Contains(raw, k.KeyHash) {
			t.Fatalf("list JSON leaked the key hash of %s", k.ID)
		}
	}
}

func TestListAPIKeysFilteredByAudienceReturnsOnlyThatAudienceWithoutSecrets(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	base := time.Now().UTC().Truncate(time.Second)
	older := insertListTestKey(t, "", owner.ID, "crazy", base.Add(-2*time.Hour))
	newer := insertListTestKey(t, "", owner.ID, "crazy", base.Add(-1*time.Hour))
	other := insertListTestKey(t, "", owner.ID, "outro", base)

	w, resp := getListAPIKeys(t, url.Values{"audience": {"crazy"}}, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertListedIDs(t, resp, newer.ID, older.ID)
	for _, k := range resp.APIKeys {
		if k.Audience != "crazy" || k.OwnerUserID != owner.ID || k.AudienceType != "text" || k.CreatedAt.IsZero() {
			t.Fatalf("unexpected projection item: %+v", k)
		}
	}
	assertNoSecretsInListJSON(t, w.Body.String(), older, newer, other)
}

func TestListAPIKeysFilteredByOwnerReturnsOnlyThatOwnersKeys(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	stranger := createTestUser(t, "stranger@example.com", false)
	base := time.Now().UTC().Truncate(time.Second)
	crazy := insertListTestKey(t, "", owner.ID, "crazy", base.Add(-2*time.Hour))
	outro := insertListTestKey(t, "", owner.ID, "outro", base.Add(-1*time.Hour))
	insertListTestKey(t, "", stranger.ID, "crazy", base)

	w, resp := getListAPIKeys(t, url.Values{"owner_user_id": {owner.ID}}, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertListedIDs(t, resp, outro.ID, crazy.ID)
	assertNoSecretsInListJSON(t, w.Body.String(), crazy, outro)
}

func TestListAPIKeysFilteredByOwnerAndAudienceCombinesWithAnd(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	stranger := createTestUser(t, "stranger@example.com", false)
	base := time.Now().UTC().Truncate(time.Second)
	match := insertListTestKey(t, "", owner.ID, "crazy", base.Add(-3*time.Hour))
	insertListTestKey(t, "", owner.ID, "outro", base.Add(-2*time.Hour))
	insertListTestKey(t, "", stranger.ID, "crazy", base.Add(-1*time.Hour))

	w, resp := getListAPIKeys(t, url.Values{"owner_user_id": {owner.ID}, "audience": {"crazy"}}, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertListedIDs(t, resp, match.ID)
}

func TestListAPIKeysWithoutFiltersReturnsAllKeys(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	stranger := createTestUser(t, "stranger@example.com", false)
	base := time.Now().UTC().Truncate(time.Second)
	a := insertListTestKey(t, "", owner.ID, "crazy", base.Add(-2*time.Hour))
	b := insertListTestKey(t, "", stranger.ID, "outro", base.Add(-1*time.Hour))

	w, resp := getListAPIKeys(t, nil, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertListedIDs(t, resp, b.ID, a.ID)
	assertNoSecretsInListJSON(t, w.Body.String(), a, b)
}

func TestListAPIKeysOrderedByCreatedAtDescThenIDDesc(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	base := time.Now().UTC().Truncate(time.Second)
	// Inserted out of order; two keys share created_at so the id DESC tie-break decides.
	oldest := insertListTestKey(t, "", owner.ID, "crazy", base.Add(-48*time.Hour))
	tieLow := insertListTestKey(t, "00000000-0000-4000-8000-000000000001", owner.ID, "crazy", base.Add(-1*time.Hour))
	newest := insertListTestKey(t, "", owner.ID, "crazy", base)
	tieHigh := insertListTestKey(t, "ffffffff-ffff-4fff-bfff-ffffffffffff", owner.ID, "crazy", base.Add(-1*time.Hour))

	w, resp := getListAPIKeys(t, url.Values{"audience": {"crazy"}}, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertListedIDs(t, resp, newest.ID, tieHigh.ID, tieLow.ID, oldest.ID)
}

func TestListAPIKeysNoMatchReturnsEmptyArrayNotNull(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	insertListTestKey(t, "", owner.ID, "crazy", time.Now().UTC())

	cases := map[string]url.Values{
		"empty table audience": {"audience": {"nenhuma"}},
		"unknown owner uuid":   {"owner_user_id": {uuid.New().String()}},
	}
	for name, query := range cases {
		w, resp := getListAPIKeys(t, query, "Bearer "+TEST_SERVICE_KEY)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", name, w.Code, w.Body.String())
		}
		if !strings.Contains(strings.ReplaceAll(w.Body.String(), " ", ""), `"api_keys":[]`) {
			t.Fatalf("%s: expected \"api_keys\":[] in raw JSON, got %s", name, w.Body.String())
		}
		if len(resp.APIKeys) != 0 {
			t.Fatalf("%s: expected no items, got %d", name, len(resp.APIKeys))
		}
	}
}

func TestListAPIKeysIncludesRevokedAndExpiredKeysWithTheirTimestamps(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	base := time.Now().UTC().Truncate(time.Second)
	revoked := insertListTestKey(t, "", owner.ID, "crazy", base.Add(-2*time.Hour))
	expired := insertListTestKey(t, "", owner.ID, "crazy", base.Add(-1*time.Hour))
	revokedAt := base.Add(-30 * time.Minute)
	expiredAt := base.Add(-10 * time.Minute)
	if err := db.DB().Model(revoked).Update("revoked_at", revokedAt).Error; err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := db.DB().Model(expired).Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire: %v", err)
	}

	w, resp := getListAPIKeys(t, url.Values{"audience": {"crazy"}}, "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertListedIDs(t, resp, expired.ID, revoked.ID)
	if resp.APIKeys[0].ExpiresAt == nil || !resp.APIKeys[0].ExpiresAt.Equal(expiredAt) {
		t.Fatalf("expected expires_at %s on expired key, got %v", expiredAt, resp.APIKeys[0].ExpiresAt)
	}
	if resp.APIKeys[1].RevokedAt == nil || !resp.APIKeys[1].RevokedAt.Equal(revokedAt) {
		t.Fatalf("expected revoked_at %s on revoked key, got %v", revokedAt, resp.APIKeys[1].RevokedAt)
	}
}

func TestListAPIKeysNonUUIDOwnerReturns400InvalidRequest(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	insertListTestKey(t, "", owner.ID, "crazy", time.Now().UTC())

	for _, bad := range []string{"abc", "123", "' OR '1'='1", owner.ID + "x"} {
		w, resp := getListAPIKeys(t, url.Values{"owner_user_id": {bad}}, "Bearer "+TEST_SERVICE_KEY)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("owner_user_id=%q: expected 400, got %d: %s", bad, w.Code, w.Body.String())
		}
		if resp.Error != "invalid request" {
			t.Fatalf("owner_user_id=%q: expected error \"invalid request\", got %q", bad, resp.Error)
		}
	}
}

func TestListAPIKeysAudienceInjectionIsTreatedAsLiteral(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	insertListTestKey(t, "", owner.ID, "crazy", time.Now().UTC())

	for _, payload := range []string{"' OR '1'='1", "crazy' --", "%"} {
		w, resp := getListAPIKeys(t, url.Values{"audience": {payload}}, "Bearer "+TEST_SERVICE_KEY)
		if w.Code != http.StatusOK {
			t.Fatalf("audience=%q: expected 200, got %d: %s", payload, w.Code, w.Body.String())
		}
		if len(resp.APIKeys) != 0 {
			t.Fatalf("audience=%q: expected literal match (no items), got %d", payload, len(resp.APIKeys))
		}
	}
}

func TestListAPIKeysWithoutServiceKeyReturns401(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	insertListTestKey(t, "", owner.ID, "crazy", time.Now().UTC())

	for _, header := range []string{"", "Bearer wrong-service-key", TEST_SERVICE_KEY} {
		w, resp := getListAPIKeys(t, url.Values{"audience": {"crazy"}}, header)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Authorization=%q: expected 401, got %d: %s", header, w.Code, w.Body.String())
		}
		if len(resp.APIKeys) != 0 {
			t.Fatalf("Authorization=%q: no key may be listed without a valid service key", header)
		}
	}
}

func TestListAPIKeysRawKeyAndHashNeverInStdout(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	rawKey := "jns_" + models.GenerateOpaqueToken()
	stored := &models.APIKey{OwnerUserID: owner.ID, Audience: "crazy", AudienceType: "text", KeyHash: models.HashToken(rawKey)}
	if err := db.DB().Create(stored).Error; err != nil {
		t.Fatalf("insert api key: %v", err)
	}
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

	w, resp := getListAPIKeys(t, url.Values{"audience": {"crazy"}}, "Bearer "+TEST_SERVICE_KEY)

	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	out := <-captured

	if w.Code != http.StatusOK || len(resp.APIKeys) != 1 {
		t.Fatalf("expected 200 with 1 item, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(out, rawKey) || strings.Contains(out, strings.TrimPrefix(rawKey, "jns_")) {
		t.Fatal("raw api key leaked to stdout during list")
	}
	if strings.Contains(out, stored.KeyHash) {
		t.Fatal("key hash leaked to stdout during list")
	}
}
