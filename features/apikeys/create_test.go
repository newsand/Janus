package apikeys

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"github.com/newsand/janus/internal/models"
)

var createdRawKeyPattern = regexp.MustCompile(`^jns_[0-9a-f]{64}$`)

type createAPIKeyResponse struct {
	Key    string `json:"key"`
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

// postCreateAPIKey sends POST /v1/api-keys with a raw JSON body; authHeader "" omits the header.
func postCreateAPIKey(t *testing.T, body string, authHeader string) (*httptest.ResponseRecorder, createAPIKeyResponse) {
	t.Helper()
	router := setupTestRouter()
	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp createAPIKeyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Logf("response body is not JSON (status %d): %s", w.Code, w.Body.String())
	}
	return w, resp
}

func createBody(t *testing.T, fields map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(raw)
}

func countAPIKeys(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := db.DB().Model(&models.APIKey{}).Count(&n).Error; err != nil {
		t.Fatalf("count api keys: %v", err)
	}
	return n
}

func assertExpiresAround(t *testing.T, got *time.Time, before, after time.Time, days int) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected expires_at ≈ now+%dd, got null", days)
	}
	span := time.Duration(days) * 24 * time.Hour
	lo := before.Add(span).Add(-2 * time.Second)
	hi := after.Add(span).Add(2 * time.Second)
	if got.Before(lo) || got.After(hi) {
		t.Fatalf("expected expires_at in [%s, %s], got %s", lo, hi, got)
	}
}

func TestCreateAPIKeyReturns201WithRawKeyOnceAndStoresOnlyHash(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)

	before := time.Now()
	w, resp := postCreateAPIKey(t, createBody(t, map[string]any{
		"owner_user_id":   owner.ID,
		"audience":        "crazy",
		"expires_in_days": 30,
		"name":            "ERP",
	}), "Bearer "+TEST_SERVICE_KEY)
	after := time.Now()

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if !createdRawKeyPattern.MatchString(resp.Key) {
		t.Fatalf("expected key jns_<64 hex>, got %q", resp.Key)
	}
	if strings.Contains(w.Body.String(), "key_hash") {
		t.Fatalf("response must not expose key_hash: %s", w.Body.String())
	}
	if strings.Count(w.Body.String(), resp.Key) != 1 {
		t.Fatalf("raw key must appear exactly once in the response: %s", w.Body.String())
	}

	if _, err := uuid.Parse(resp.APIKey.ID); err != nil {
		t.Fatalf("expected api_key.id UUID, got %q", resp.APIKey.ID)
	}
	if resp.APIKey.OwnerUserID != owner.ID || resp.APIKey.Audience != "crazy" || resp.APIKey.AudienceType != "text" {
		t.Fatalf("unexpected metadata: %+v", resp.APIKey)
	}
	if resp.APIKey.Name == nil || *resp.APIKey.Name != "ERP" {
		t.Fatalf("expected name ERP, got %v", resp.APIKey.Name)
	}
	if resp.APIKey.RevokedAt != nil {
		t.Fatalf("expected revoked_at null, got %v", resp.APIKey.RevokedAt)
	}
	if resp.APIKey.CreatedAt.IsZero() {
		t.Fatal("expected created_at set")
	}
	assertExpiresAround(t, resp.APIKey.ExpiresAt, before, after, 30)

	var stored models.APIKey
	if err := db.DB().First(&stored, "id = ?", resp.APIKey.ID).Error; err != nil {
		t.Fatalf("stored api key not found: %v", err)
	}
	if stored.KeyHash != models.HashToken(resp.Key) {
		t.Fatalf("expected key_hash = HashToken(key), got %q", stored.KeyHash)
	}
	if stored.KeyHash == resp.Key || strings.Contains(stored.KeyHash, "jns_") {
		t.Fatal("raw key must not be stored")
	}
	if stored.OwnerUserID != owner.ID || stored.Audience != "crazy" || stored.AudienceType != "text" {
		t.Fatalf("unexpected stored row: %+v", stored)
	}
	assertExpiresAround(t, stored.ExpiresAt, before, after, 30)
	if countAPIKeys(t) != 1 {
		t.Fatalf("expected exactly 1 row, got %d", countAPIKeys(t))
	}
}

func TestCreateAPIKeyExplicitTextAudienceTypeReturns201(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)

	w, resp := postCreateAPIKey(t, createBody(t, map[string]any{
		"owner_user_id": owner.ID,
		"audience":      "crazy",
		"audience_type": "text",
	}), "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.AudienceType != "text" {
		t.Fatalf("expected audience_type text, got %q", resp.APIKey.AudienceType)
	}
}

func TestCreateAPIKeyExpiresInDaysBoundariesAccepted(t *testing.T) {
	for _, days := range []int{1, 365} {
		setupTestDB(t)
		owner := createTestUser(t, "owner@example.com", false)

		before := time.Now()
		w, resp := postCreateAPIKey(t, createBody(t, map[string]any{
			"owner_user_id":   owner.ID,
			"audience":        "crazy",
			"expires_in_days": days,
		}), "Bearer "+TEST_SERVICE_KEY)
		after := time.Now()

		if w.Code != http.StatusCreated {
			t.Fatalf("expires_in_days=%d: expected 201, got %d: %s", days, w.Code, w.Body.String())
		}
		assertExpiresAround(t, resp.APIKey.ExpiresAt, before, after, days)
	}
}

func TestCreateAPIKeyWithoutExpiresInDaysHasNullExpiresAt(t *testing.T) {
	cases := map[string]map[string]any{
		"absent": {"audience": "crazy"},
		"null":   {"audience": "crazy", "expires_in_days": nil},
	}
	for name, fields := range cases {
		setupTestDB(t)
		owner := createTestUser(t, "owner@example.com", false)
		fields["owner_user_id"] = owner.ID

		w, resp := postCreateAPIKey(t, createBody(t, fields), "Bearer "+TEST_SERVICE_KEY)

		if w.Code != http.StatusCreated {
			t.Fatalf("%s: expected 201, got %d: %s", name, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"expires_at":null`) {
			t.Fatalf("%s: expected expires_at null in JSON, got %s", name, w.Body.String())
		}
		var stored models.APIKey
		if err := db.DB().First(&stored, "id = ?", resp.APIKey.ID).Error; err != nil {
			t.Fatalf("%s: stored api key not found: %v", name, err)
		}
		if stored.ExpiresAt != nil {
			t.Fatalf("%s: expected stored expires_at NULL, got %v", name, stored.ExpiresAt)
		}
	}
}

func TestCreateAPIKeyExpiresInDaysOutOfRangeReturns400(t *testing.T) {
	for _, raw := range []string{"0", "366", "-1", "1.5"} {
		setupTestDB(t)
		owner := createTestUser(t, "owner@example.com", false)
		body := `{"owner_user_id":"` + owner.ID + `","audience":"crazy","expires_in_days":` + raw + `}`

		w, resp := postCreateAPIKey(t, body, "Bearer "+TEST_SERVICE_KEY)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expires_in_days=%s: expected 400, got %d: %s", raw, w.Code, w.Body.String())
		}
		if resp.Error != "expires_in_days must be between 1 and 365" {
			t.Fatalf("expires_in_days=%s: unexpected error %q", raw, resp.Error)
		}
		if countAPIKeys(t) != 0 {
			t.Fatalf("expires_in_days=%s: no row must be created", raw)
		}
	}
}

func TestCreateAPIKeyInvalidAudienceTypeReturns400(t *testing.T) {
	for _, audienceType := range []string{"regex", "TEXT", ""} {
		setupTestDB(t)
		owner := createTestUser(t, "owner@example.com", false)

		w, resp := postCreateAPIKey(t, createBody(t, map[string]any{
			"owner_user_id": owner.ID,
			"audience":      "crazy",
			"audience_type": audienceType,
		}), "Bearer "+TEST_SERVICE_KEY)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("audience_type=%q: expected 400, got %d: %s", audienceType, w.Code, w.Body.String())
		}
		if resp.Error != "invalid audience_type" {
			t.Fatalf("audience_type=%q: unexpected error %q", audienceType, resp.Error)
		}
		if countAPIKeys(t) != 0 {
			t.Fatalf("audience_type=%q: no row must be created", audienceType)
		}
	}
}

func TestCreateAPIKeyInvalidRequestReturns400(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	long := strings.Repeat("a", 101)

	cases := map[string]string{
		"malformed json":   `{"owner_user_id":`,
		"missing owner":    createBody(t, map[string]any{"audience": "crazy"}),
		"missing audience": createBody(t, map[string]any{"owner_user_id": owner.ID}),
		"empty audience":   createBody(t, map[string]any{"owner_user_id": owner.ID, "audience": ""}),
		"audience > 100":   createBody(t, map[string]any{"owner_user_id": owner.ID, "audience": long}),
		"name > 100":       createBody(t, map[string]any{"owner_user_id": owner.ID, "audience": "crazy", "name": long}),
	}
	for name, body := range cases {
		w, resp := postCreateAPIKey(t, body, "Bearer "+TEST_SERVICE_KEY)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", name, w.Code, w.Body.String())
		}
		if resp.Error != "invalid request" {
			t.Fatalf("%s: expected error \"invalid request\", got %q", name, resp.Error)
		}
	}
	if countAPIKeys(t) != 0 {
		t.Fatalf("no row must be created, got %d", countAPIKeys(t))
	}
}

func TestCreateAPIKeyLengthBoundariesAccepted(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	hundred := strings.Repeat("a", 100)

	w, resp := postCreateAPIKey(t, createBody(t, map[string]any{
		"owner_user_id": owner.ID,
		"audience":      hundred,
		"name":          hundred,
	}), "Bearer "+TEST_SERVICE_KEY)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for 100-char audience and name, got %d: %s", w.Code, w.Body.String())
	}
	if resp.APIKey.Audience != hundred || resp.APIKey.Name == nil || *resp.APIKey.Name != hundred {
		t.Fatalf("unexpected metadata: %+v", resp.APIKey)
	}
}

func TestCreateAPIKeyUnknownOrDisabledOwnerReturns400InvalidOwner(t *testing.T) {
	setupTestDB(t)
	disabled := createTestUser(t, "disabled@example.com", true)

	cases := map[string]string{
		"unknown owner":  uuid.New().String(),
		"disabled owner": disabled.ID,
	}
	for name, ownerID := range cases {
		w, resp := postCreateAPIKey(t, createBody(t, map[string]any{
			"owner_user_id":   ownerID,
			"audience":        "crazy",
			"expires_in_days": 30,
		}), "Bearer "+TEST_SERVICE_KEY)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d: %s", name, w.Code, w.Body.String())
		}
		if resp.Error != "invalid owner" {
			t.Fatalf("%s: expected error \"invalid owner\", got %q", name, resp.Error)
		}
	}
	if countAPIKeys(t) != 0 {
		t.Fatalf("no row must be created, got %d", countAPIKeys(t))
	}
}

func TestCreateAPIKeyWithoutServiceKeyReturns401(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	body := createBody(t, map[string]any{"owner_user_id": owner.ID, "audience": "crazy"})

	for _, header := range []string{"", "Bearer wrong-service-key", TEST_SERVICE_KEY} {
		w, _ := postCreateAPIKey(t, body, header)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Authorization=%q: expected 401, got %d: %s", header, w.Code, w.Body.String())
		}
	}
	if countAPIKeys(t) != 0 {
		t.Fatalf("no row must be created without a valid service key, got %d", countAPIKeys(t))
	}
}

func TestCreateAPIKeyTwoCallsCreateDistinctKeys(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	body := createBody(t, map[string]any{"owner_user_id": owner.ID, "audience": "crazy"})

	w1, first := postCreateAPIKey(t, body, "Bearer "+TEST_SERVICE_KEY)
	w2, second := postCreateAPIKey(t, body, "Bearer "+TEST_SERVICE_KEY)

	if w1.Code != http.StatusCreated || w2.Code != http.StatusCreated {
		t.Fatalf("expected 201 twice, got %d and %d", w1.Code, w2.Code)
	}
	if first.Key == second.Key || first.APIKey.ID == second.APIKey.ID {
		t.Fatalf("expected distinct keys and ids, got %q/%q and %q/%q", first.Key, first.APIKey.ID, second.Key, second.APIKey.ID)
	}
	if countAPIKeys(t) != 2 {
		t.Fatalf("expected 2 rows, got %d", countAPIKeys(t))
	}
}

func TestCreateAPIKeyRawKeyNeverInStdout(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
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

	w, resp := postCreateAPIKey(t, createBody(t, map[string]any{
		"owner_user_id":   owner.ID,
		"audience":        "crazy",
		"expires_in_days": 30,
		"name":            "ERP",
	}), "Bearer "+TEST_SERVICE_KEY)

	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	out := <-captured

	if w.Code != http.StatusCreated || resp.Key == "" {
		t.Fatalf("expected 201 with a key, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(out, resp.Key) {
		t.Fatal("raw api key leaked to stdout during create")
	}
	if strings.Contains(out, strings.TrimPrefix(resp.Key, "jns_")) {
		t.Fatal("raw api key secret leaked to stdout during create")
	}
}
