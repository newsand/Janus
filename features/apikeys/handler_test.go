package apikeys

import (
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TEST_SERVICE_KEY is the service key accepted by ServiceKeyAuth in every apikeys test.
const TEST_SERVICE_KEY = "test-service-key"

// setupTestDB swaps the singleton DB for an in-memory SQLite with users + api_keys.
func setupTestDB(t *testing.T) {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	if err := testDB.AutoMigrate(&models.User{}, &models.APIKey{}); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	db.SetTestDB(testDB)
}

// setupTestRouter mounts the apikeys routes under /v1 with TEST_SERVICE_KEY configured.
func setupTestRouter() *gin.Engine {
	config.Load()
	config.Get().ServiceKeys = []string{TEST_SERVICE_KEY}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r.Group("/v1"))
	return r
}

// createTestUser inserts a user; disabled=true sets disabled_at.
func createTestUser(t *testing.T, email string, disabled bool) *models.User {
	t.Helper()
	user := &models.User{Email: email, Nome: "Test User"}
	if disabled {
		now := time.Now()
		user.DisabledAt = &now
	}
	if err := db.DB().Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	return user
}

func TestAPIKeyBeforeCreateAssignsUUID(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)

	key := &models.APIKey{
		OwnerUserID:  owner.ID,
		Audience:     "crazy",
		AudienceType: "text",
		KeyHash:      models.HashToken("jns_t01-before-create"),
	}
	if err := db.DB().Create(key).Error; err != nil {
		t.Fatalf("create api key: %v", err)
	}

	parsed, err := uuid.Parse(key.ID)
	if err != nil {
		t.Fatalf("expected UUID id, got %q: %v", key.ID, err)
	}
	if parsed.Version() != 4 {
		t.Fatalf("expected UUID v4, got version %d", parsed.Version())
	}

	var stored models.APIKey
	if err := db.DB().First(&stored, "id = ?", key.ID).Error; err != nil {
		t.Fatalf("stored api key not found by id: %v", err)
	}
	if stored.CreatedAt.IsZero() {
		t.Fatal("expected created_at to be set on create")
	}
	if stored.ExpiresAt != nil || stored.RevokedAt != nil || stored.Name != nil {
		t.Fatalf("expected nullable fields to stay NULL, got expires_at=%v revoked_at=%v name=%v",
			stored.ExpiresAt, stored.RevokedAt, stored.Name)
	}
}

func TestAPIKeyBeforeCreateKeepsPresetID(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)

	preset := uuid.New().String()
	key := &models.APIKey{
		ID:           preset,
		OwnerUserID:  owner.ID,
		Audience:     "crazy",
		AudienceType: "text",
		KeyHash:      models.HashToken("jns_t01-preset"),
	}
	if err := db.DB().Create(key).Error; err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if key.ID != preset {
		t.Fatalf("expected preset id %q kept, got %q", preset, key.ID)
	}
}

func TestAPIKeyTwoCreatesGetDistinctIDs(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)

	first := &models.APIKey{OwnerUserID: owner.ID, Audience: "crazy", AudienceType: "text", KeyHash: models.HashToken("jns_t01-a")}
	second := &models.APIKey{OwnerUserID: owner.ID, Audience: "crazy", AudienceType: "text", KeyHash: models.HashToken("jns_t01-b")}
	if err := db.DB().Create(first).Error; err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := db.DB().Create(second).Error; err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.ID == "" || first.ID == second.ID {
		t.Fatalf("expected distinct non-empty ids, got %q and %q", first.ID, second.ID)
	}
}

func TestAPIKeyHashIsUnique(t *testing.T) {
	setupTestDB(t)
	owner := createTestUser(t, "owner@example.com", false)
	other := createTestUser(t, "other@example.com", false)
	hash := models.HashToken("jns_t01-duplicate")

	// Distinct preset IDs: only key_hash can collide (RED re-spawn: empty ids collided on the PK).
	first := &models.APIKey{ID: uuid.New().String(), OwnerUserID: owner.ID, Audience: "crazy", AudienceType: "text", KeyHash: hash}
	if err := db.DB().Create(first).Error; err != nil {
		t.Fatalf("create first: %v", err)
	}

	dup := &models.APIKey{ID: uuid.New().String(), OwnerUserID: other.ID, Audience: "outro", AudienceType: "text", KeyHash: hash}
	err := db.DB().Create(dup).Error
	if err == nil {
		t.Fatal("expected unique violation on duplicate key_hash, got nil error")
	}
	if !strings.Contains(err.Error(), "key_hash") {
		t.Fatalf("expected the unique violation to concern key_hash, got: %v", err)
	}

	var count int64
	db.DB().Model(&models.APIKey{}).Where("key_hash = ?", hash).Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 row with the hash, got %d", count)
	}
}

func TestAPIKeyRegisterRoutesMountsWithoutPanic(t *testing.T) {
	setupTestDB(t)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RegisterRoutes panicked: %v", r)
		}
	}()
	if setupTestRouter() == nil {
		t.Fatal("expected a router")
	}
}
