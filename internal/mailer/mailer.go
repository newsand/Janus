package mailer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/newsand/janus/internal/config"
)

const (
	LinkPlaceholder = "{{link}}"
	MaxSubjectLen   = 200
	MaxBodyLen      = 10000
)

var (
	ErrMissingLink = errors.New("body must contain {{link}}")
	ErrTooLong     = errors.New("subject or body exceeds max length")
	ErrUnknownKind = errors.New("unknown mail kind")
)

type Message struct {
	To      string
	Subject string
	Body    string
}

type sender interface {
	Send(p_Msg Message) error
}

var (
	g_Sender      sender
	g_AppBaseURL  string
	g_Stub        bool
	g_MaxAttempts int
	g_Cancel      context.CancelFunc
	g_Mu          sync.Mutex
)

func Init(p_Cfg *config.Config) {
	g_Mu.Lock()
	defer g_Mu.Unlock()

	if g_Cancel != nil {
		g_Cancel()
		g_Cancel = nil
	}

	g_AppBaseURL = strings.TrimRight(p_Cfg.AppBaseURL, "/")
	g_Stub = p_Cfg.MailerStub
	g_MaxAttempts = p_Cfg.MailerMaxAttempts
	if g_MaxAttempts <= 0 {
		g_MaxAttempts = 5
	}

	if p_Cfg.MailerStub {
		g_Sender = stubSender{}
		return
	}

	g_Sender = smtpSender{
		Host:      p_Cfg.SMTPHost,
		Port:      p_Cfg.SMTPPort,
		User:      p_Cfg.SMTPUser,
		Pass:      p_Cfg.SMTPPass,
		FromName:  p_Cfg.MailFromName,
		FromEmail: p_Cfg.MailFromEmail,
	}

	ctx, cancel := context.WithCancel(context.Background())
	g_Cancel = cancel
	interval := p_Cfg.MailerWorkerInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	startWorker(ctx, interval)
}

func Stop() {
	g_Mu.Lock()
	defer g_Mu.Unlock()
	if g_Cancel != nil {
		g_Cancel()
		g_Cancel = nil
	}
}

func Stub() bool {
	ensureInit()
	return g_Stub
}

func ensureInit() {
	if g_Sender == nil {
		Init(config.Get())
	}
}

func Prepare(p_Kind, p_Email, p_Subject, p_Body, p_Token string) (Message, error) {
	ensureInit()
	path, err := pathForKind(p_Kind)
	if err != nil {
		return Message{}, err
	}
	if len(p_Subject) > MaxSubjectLen || len(p_Body) > MaxBodyLen {
		return Message{}, ErrTooLong
	}
	if !strings.Contains(p_Body, LinkPlaceholder) {
		return Message{}, ErrMissingLink
	}
	rendered := render(p_Body, map[string]string{"link": link(path, p_Token)})
	if strings.Contains(rendered, LinkPlaceholder) {
		return Message{}, ErrMissingLink
	}
	return Message{
		To:      p_Email,
		Subject: p_Subject,
		Body:    rendered,
	}, nil
}

func PrepareInvite(p_Email, p_Subject, p_Body, p_Token string) (Message, error) {
	return Prepare(KindInvite, p_Email, p_Subject, p_Body, p_Token)
}

func PrepareMagicLink(p_Email, p_Subject, p_Body, p_Token string) (Message, error) {
	return Prepare(KindMagicLink, p_Email, p_Subject, p_Body, p_Token)
}

func PrepareRecover(p_Email, p_Subject, p_Body, p_Token string) (Message, error) {
	return Prepare(KindRecover, p_Email, p_Subject, p_Body, p_Token)
}

func pathForKind(p_Kind string) (string, error) {
	switch p_Kind {
	case KindInvite:
		return "/invite", nil
	case KindMagicLink:
		return "/magic-link", nil
	case KindRecover:
		return "/password/reset", nil
	default:
		return "", ErrUnknownKind
	}
}

func link(p_Path, p_Token string) string {
	base := g_AppBaseURL
	if base == "" {
		base = "http://localhost"
	}
	return base + p_Path + "?token=" + p_Token
}

func render(p_Tpl string, p_Vars map[string]string) string {
	out := p_Tpl
	for k, v := range p_Vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}
