package mailer

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/newsand/janus/internal/config"
	"github.com/newsand/janus/internal/db"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupMailerDB(t *testing.T) {
	t.Helper()
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	if err := testDB.AutoMigrate(&Outbox{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.SetTestDB(testDB)
	Init(&config.Config{
		MailerStub:        true,
		AppBaseURL:        "https://app.example.com",
		MailerMaxAttempts: 5,
	})
}

func TestRender(t *testing.T) {
	got := render("Oi {{nome}}, link={{link}}", map[string]string{
		"nome": "Ana",
		"link": "https://app.example.com/x?token=abc",
	})
	want := "Oi Ana, link=https://app.example.com/x?token=abc"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPrepareMissingLink(t *testing.T) {
	Init(&config.Config{MailerStub: true, AppBaseURL: "https://app.example.com"})
	_, err := PrepareInvite("u@example.com", "Assunto", "sem placeholder", "tok")
	if !errors.Is(err, ErrMissingLink) {
		t.Fatalf("want ErrMissingLink got %v", err)
	}
}

func TestPrepareRendersLink(t *testing.T) {
	Init(&config.Config{MailerStub: true, AppBaseURL: "https://app.example.com"})
	msg, err := PrepareInvite("u@example.com", "Convite", "Aceite: {{link}}", "tok123")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !strings.Contains(msg.Body, "https://app.example.com/invite?token=tok123") {
		t.Fatalf("body: %s", msg.Body)
	}
	if strings.Contains(msg.Body, LinkPlaceholder) {
		t.Fatal("placeholder must be gone after Prepare")
	}
}

func TestEnqueueInviteStubMarksSent(t *testing.T) {
	setupMailerDB(t)

	err := EnqueueInvite(nil, "user@example.com", "Convite", "Aceite: {{link}}", "tok123")
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var row Outbox
	if err := db.DB().First(&row).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if row.Status != StatusSent {
		t.Fatalf("status=%s want sent", row.Status)
	}
	if row.Kind != KindInvite {
		t.Fatalf("kind=%s", row.Kind)
	}
	if !strings.Contains(row.Body, "https://app.example.com/invite?token=tok123") {
		t.Fatalf("body: %s", row.Body)
	}
}

func TestEnqueueBlindAcceptsPreparedMessage(t *testing.T) {
	setupMailerDB(t)
	msg, err := PrepareInvite("user@example.com", "Convite", "Aceite: {{link}}", "tok")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := Enqueue(nil, KindInvite, msg); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	var n int64
	db.DB().Model(&Outbox{}).Count(&n)
	if n != 1 {
		t.Fatalf("rows=%d", n)
	}
}

func TestEnqueueInviteRejectsMissingLink(t *testing.T) {
	setupMailerDB(t)
	err := EnqueueInvite(nil, "user@example.com", "Convite", "sem link", "tok")
	if !errors.Is(err, ErrMissingLink) {
		t.Fatalf("want ErrMissingLink got %v", err)
	}
	var n int64
	db.DB().Model(&Outbox{}).Count(&n)
	if n != 0 {
		t.Fatalf("expected no outbox rows, got %d", n)
	}
}

type failSender struct{}

func (failSender) Send(p_Msg Message) error {
	return errors.New("smtp down")
}

func TestProcessRowFailsPermanently(t *testing.T) {
	setupMailerDB(t)
	g_Sender = failSender{}
	g_Stub = false

	row := Outbox{
		Kind:        KindInvite,
		ToEmail:     "u@example.com",
		Subject:     "s",
		Body:        "b",
		Status:      StatusSending,
		Attempts:    5,
		MaxAttempts: 5,
		AvailableAt: time.Now(),
	}
	if err := db.DB().Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := processRow(db.DB(), &row); err != nil {
		t.Fatalf("process: %v", err)
	}
	var got Outbox
	db.DB().First(&got, "id = ?", row.ID)
	if got.Status != StatusFailed {
		t.Fatalf("status=%s want failed", got.Status)
	}
}

func TestBuildMessage(t *testing.T) {
	raw := buildMessage("Janus", "noreply@example.com", Message{
		To:      "user@example.com",
		Subject: "Convite",
		Body:    "corpo",
	})
	for _, part := range []string{
		"From: Janus <noreply@example.com>",
		"To: user@example.com",
		"Subject: Convite",
		"Content-Type: text/plain; charset=UTF-8",
		"corpo",
	} {
		if !strings.Contains(raw, part) {
			t.Fatalf("missing %q in:\n%s", part, raw)
		}
	}
}
