package config

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	long := strings.Repeat("a", MinSecretLength)
	short := strings.Repeat("a", MinSecretLength-1)

	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"dev accepts defaults", Config{DevEnv: true, JWTSecret: "change-me-in-production"}, false},
		{"dev accepts empty keys", Config{DevEnv: true, JWTSecret: "x"}, false},
		{"prod ok", Config{JWTSecret: long, ServiceKeys: []string{long, long}, MailerStub: true}, false},
		{"prod default jwt", Config{JWTSecret: "change-me-in-production", ServiceKeys: []string{long}}, true},
		{"prod short jwt", Config{JWTSecret: short, ServiceKeys: []string{long}}, true},
		{"prod empty jwt", Config{ServiceKeys: []string{long}}, true},
		{"prod no keys", Config{JWTSecret: long}, true},
		{"prod example keys", Config{JWTSecret: long, ServiceKeys: []string{"key1", "key2"}}, true},
		{"prod one short key among long", Config{JWTSecret: long, ServiceKeys: []string{long, short}}, true},
		{"prod stub skips smtp", Config{JWTSecret: long, ServiceKeys: []string{long}, MailerStub: true}, false},
		{"prod real mailer ok", Config{
			JWTSecret: long, ServiceKeys: []string{long}, MailerStub: false,
			SMTPHost: "smtp.gmail.com", SMTPUser: "u", SMTPPass: "p",
			MailFromEmail: "u@x.com", AppBaseURL: "https://app.example.com",
		}, false},
		{"prod real mailer missing host", Config{
			JWTSecret: long, ServiceKeys: []string{long}, MailerStub: false,
			SMTPUser: "u", SMTPPass: "p", MailFromEmail: "u@x.com", AppBaseURL: "https://app.example.com",
		}, true},
		{"prod real mailer missing base url", Config{
			JWTSecret: long, ServiceKeys: []string{long}, MailerStub: false,
			SMTPHost: "smtp.gmail.com", SMTPUser: "u", SMTPPass: "p", MailFromEmail: "u@x.com",
		}, true},
		{"prod sendpulse ok", Config{
			JWTSecret: long, ServiceKeys: []string{long}, MailerStub: false, MailerProvider: "sendpulse",
			SendPulseUserID: "id", SendPulseSecret: "secret",
			MailFromEmail: "u@x.com", AppBaseURL: "https://app.example.com",
		}, false},
		{"prod sendpulse missing user id", Config{
			JWTSecret: long, ServiceKeys: []string{long}, MailerStub: false, MailerProvider: "sendpulse",
			SendPulseSecret: "secret",
			MailFromEmail:   "u@x.com", AppBaseURL: "https://app.example.com",
		}, true},
		{"prod sendpulse missing secret", Config{
			JWTSecret: long, ServiceKeys: []string{long}, MailerStub: false, MailerProvider: "sendpulse",
			SendPulseUserID: "id",
			MailFromEmail:   "u@x.com", AppBaseURL: "https://app.example.com",
		}, true},
		{"prod sendpulse doesn't require smtp vars", Config{
			JWTSecret: long, ServiceKeys: []string{long}, MailerStub: false, MailerProvider: "sendpulse",
			SendPulseUserID: "id", SendPulseSecret: "secret",
			SMTPHost:      "",
			MailFromEmail: "u@x.com", AppBaseURL: "https://app.example.com",
		}, false},
		{"prod unknown provider", Config{
			JWTSecret: long, ServiceKeys: []string{long}, MailerStub: false, MailerProvider: "carrier-pigeon",
			MailFromEmail: "u@x.com", AppBaseURL: "https://app.example.com",
		}, true},
	}
	for _, c := range cases {
		err := c.cfg.Validate()
		if (err != nil) != c.wantErr {
			t.Errorf("%s: wantErr=%v got %v", c.name, c.wantErr, err)
		}
	}
}
