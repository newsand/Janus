package mailer

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/newsand/janus/internal/db"
	"github.com/newsand/janus/internal/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	KindInvite    = "invite"
	KindMagicLink = "magic_link"
	KindRecover   = "recover"
	StatusPending = "pending"
	StatusSending = "sending"
	StatusSent    = "sent"
	StatusFailed  = "failed"
)

type Outbox struct {
	ID          string    `gorm:"type:uuid;primaryKey"`
	Kind        string    `gorm:"size:32;not null"`
	ToEmail     string    `gorm:"size:255;not null"`
	Subject     string    `gorm:"size:200;not null"`
	Body        string    `gorm:"type:text;not null"`
	Status      string    `gorm:"size:16;not null;index:idx_mail_outbox_poll,priority:1"`
	Attempts    int       `gorm:"not null;default:0"`
	MaxAttempts int       `gorm:"not null;default:5"`
	LastError   *string   `gorm:"type:text"`
	AvailableAt time.Time `gorm:"not null;index:idx_mail_outbox_poll,priority:2"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	SentAt      *time.Time
}

func (Outbox) TableName() string {
	return "mail_outbox"
}

func (o *Outbox) BeforeCreate(p_Tx *gorm.DB) error {
	if o.ID == "" {
		o.ID = uuid.New().String()
	}
	return nil
}

func Enqueue(p_Tx *gorm.DB, p_Kind string, p_Msg Message) error {
	ensureInit()
	tx := p_Tx
	if tx == nil {
		tx = db.DB()
	}

	row := Outbox{
		Kind:        p_Kind,
		ToEmail:     p_Msg.To,
		Subject:     p_Msg.Subject,
		Body:        p_Msg.Body,
		Status:      StatusPending,
		Attempts:    0,
		MaxAttempts: g_MaxAttempts,
		AvailableAt: time.Now(),
	}
	if err := tx.Create(&row).Error; err != nil {
		return fmt.Errorf("outbox insert: %w", err)
	}

	if g_Stub {
		return processRow(tx, &row)
	}
	return nil
}

func EnqueueInvite(p_Tx *gorm.DB, p_Email, p_Subject, p_Body, p_Token string) error {
	msg, err := PrepareInvite(p_Email, p_Subject, p_Body, p_Token)
	if err != nil {
		return err
	}
	return Enqueue(p_Tx, KindInvite, msg)
}

func EnqueueMagicLink(p_Tx *gorm.DB, p_Email, p_Subject, p_Body, p_Token string) error {
	msg, err := PrepareMagicLink(p_Email, p_Subject, p_Body, p_Token)
	if err != nil {
		return err
	}
	return Enqueue(p_Tx, KindMagicLink, msg)
}

func EnqueueRecover(p_Tx *gorm.DB, p_Email, p_Subject, p_Body, p_Token string) error {
	msg, err := PrepareRecover(p_Email, p_Subject, p_Body, p_Token)
	if err != nil {
		return err
	}
	return Enqueue(p_Tx, KindRecover, msg)
}

func startWorker(p_Ctx context.Context, p_Interval time.Duration) {
	ticker := time.NewTicker(p_Interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-p_Ctx.Done():
				return
			case <-ticker.C:
				if err := tickOnce(); err != nil {
					logger.Error("mailer worker: %v", err)
				}
			}
		}
	}()
}

func tickOnce() error {
	gdb := db.DB()
	if gdb == nil {
		return nil
	}
	var row Outbox
	err := gdb.Transaction(func(p_Tx *gorm.DB) error {
		err := p_Tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND available_at <= ?", StatusPending, time.Now()).
			Order("created_at ASC").
			Limit(1).
			Find(&row).Error
		if err != nil {
			return err
		}
		if row.ID == "" {
			return nil
		}
		if err := p_Tx.Model(&row).Updates(map[string]interface{}{
			"status":   StatusSending,
			"attempts": row.Attempts + 1,
		}).Error; err != nil {
			return err
		}
		row.Attempts++
		row.Status = StatusSending
		return nil
	})
	if err != nil {
		return err
	}
	if row.ID == "" {
		return nil
	}
	return processRow(gdb, &row)
}

func processRow(p_Tx *gorm.DB, p_Row *Outbox) error {
	err := g_Sender.Send(Message{
		To:      p_Row.ToEmail,
		Subject: p_Row.Subject,
		Body:    p_Row.Body,
	})
	now := time.Now()
	if err == nil {
		return p_Tx.Model(p_Row).Updates(map[string]interface{}{
			"status":     StatusSent,
			"sent_at":    now,
			"last_error": nil,
		}).Error
	}

	msg := err.Error()
	if len(msg) > 500 {
		msg = msg[:500]
	}
	if p_Row.Attempts >= p_Row.MaxAttempts {
		logger.Error("mailer outbox %s failed permanently: %s", p_Row.ID, msg)
		return p_Tx.Model(p_Row).Updates(map[string]interface{}{
			"status":     StatusFailed,
			"last_error": msg,
		}).Error
	}

	backoff := time.Duration(1<<min(p_Row.Attempts, 8)) * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	logger.Warn("mailer outbox %s attempt %d failed: %s", p_Row.ID, p_Row.Attempts, msg)
	return p_Tx.Model(p_Row).Updates(map[string]interface{}{
		"status":       StatusPending,
		"last_error":   msg,
		"available_at": now.Add(backoff),
	}).Error
}
