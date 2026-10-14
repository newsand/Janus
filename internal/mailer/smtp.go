package mailer

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type smtpSender struct {
	Host      string
	Port      string
	User      string
	Pass      string
	FromName  string
	FromEmail string
}

func (s smtpSender) Send(p_Msg Message) error {
	addr := net.JoinHostPort(s.Host, s.Port)

	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("client: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); !ok {
		return fmt.Errorf("server does not offer STARTTLS")
	}
	if err := client.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return fmt.Errorf("starttls: %w", err)
	}

	auth := smtp.PlainAuth("", s.User, strings.ReplaceAll(s.Pass, " ", ""), s.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := client.Mail(s.FromEmail); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := client.Rcpt(p_Msg.To); err != nil {
		return fmt.Errorf("rcpt: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write([]byte(buildMessage(s.FromName, s.FromEmail, p_Msg))); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	return client.Quit()
}

func buildMessage(p_FromName, p_FromEmail string, p_Msg Message) string {
	from := p_FromEmail
	if p_FromName != "" {
		from = fmt.Sprintf("%s <%s>", p_FromName, p_FromEmail)
	}
	return "From: " + from + "\r\n" +
		"To: " + p_Msg.To + "\r\n" +
		"Subject: " + p_Msg.Subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		p_Msg.Body
}
