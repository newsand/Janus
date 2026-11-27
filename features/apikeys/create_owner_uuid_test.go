package apikeys

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ownerUUIDSetupDBWithoutUsers swaps the singleton DB for an in-memory SQLite
// with api_keys only: any owner lookup errors, like Postgres casting a
// non-UUID to uuid, so a request that reaches the lookup answers 500.
func ownerUUIDSetupDBWithoutUsers(t *testing.T) {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	if err := testDB.AutoMigrate(&models.APIKey{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	db.SetTestDB(testDB)
}

// ownerUUIDPost sends POST /v1/api-keys with TEST_SERVICE_KEY and returns the recorder and decoded error.
func ownerUUIDPost(t *testing.T, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	router := setupTestRouter()
	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+TEST_SERVICE_KEY)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Logf("response body is not JSON (status %d): %s", w.Code, w.Body.String())
	}
	return w, resp.Error
}

func ownerUUIDBody(t *testing.T, fields map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(raw)
}

func ownerUUIDCountKeys(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := db.DB().Model(&models.APIKey{}).Count(&n).Error; err != nil {
		t.Fatalf("count api keys: %v", err)
	}
	return n
}

func ownerUUIDAssert400(t *testing.T, w *httptest.ResponseRecorder, gotErr, wantErr, label string) {
	t.Helper()
	if w.Code != http.StatusBadRequest || gotErr != wantErr {
		t.Errorf("%s: expected 400 %q, got %d: %s", label, wantErr, w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), rawKeyPrefix) {
		t.Errorf("%s: body must not contain a raw key: %s", label, w.Body.String())
	}
	if n := ownerUUIDCountKeys(t); n != 0 {
		t.Errorf("%s: expected no api_keys row, got %d", label, n)
	}
}

// Regression for review-W5 TASK 7: non-UUID owner_user_id reached the owner lookup and answered 500 on Postgres.
func TestCreateAPIKeyNonUUIDOwnerReturns400InvalidOwner(t *testing.T) {
	cases := map[string]string{
		"plain text":     "not-a-uuid",
		"sql injection":  "1 OR 1=1",
		"truncated uuid": "123e4567-e89b-12d3-a456-42661417400",
	}
	for label, owner := range cases {
		ownerUUIDSetupDBWithoutUsers(t)
		w, gotErr := ownerUUIDPost(t, ownerUUIDBody(t, map[string]any{"owner_user_id": owner, "audience": "crazy"}))
		ownerUUIDAssert400(t, w, gotErr, "invalid owner", label)
	}
}

func TestCreateAPIKeyEmptyOrMissingOwnerStillReturns400InvalidRequest(t *testing.T) {
	cases := map[string]map[string]any{
		"empty owner":   {"owner_user_id": "", "audience": "crazy"},
		"missing owner": {"audience": "crazy"},
	}
	for label, fields := range cases {
		ownerUUIDSetupDBWithoutUsers(t)
		w, gotErr := ownerUUIDPost(t, ownerUUIDBody(t, fields))
		ownerUUIDAssert400(t, w, gotErr, "invalid request", label)
	}
}
