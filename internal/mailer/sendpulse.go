package mailer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// SendPulse API base. Same endpoints used by modulo-financeiro's
// sendpulse-mail-helper.ts, kept in sync deliberately.
const (
	sendpulseAPI     = "https://api.sendpulse.com"
	sendpulseTimeout = 15 * time.Second
	// Refresh the OAuth token a bit before SendPulse's 3600s expiry so a
	// borderline-expired token is never handed to /smtp/emails.
	sendpulseTokenSkew = 5 * time.Minute
)

type sendpulseSender struct {
	UserID    string
	Secret    string
	FromName  string
	FromEmail string

	httpClient *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

type sendpulseTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type sendpulseContact struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

type sendpulseEmailPayload struct {
	HTML    string             `json:"html"`
	Text    string             `json:"text"`
	Subject string             `json:"subject"`
	From    sendpulseContact   `json:"from"`
	To      []sendpulseContact `json:"to"`
}

type sendpulseEmailRequest struct {
	Email sendpulseEmailPayload `json:"email"`
}

type sendpulseEmailResponse struct {
	Result bool `json:"result"`
}

func (s *sendpulseSender) client() *http.Client {
	if s.httpClient != nil {
		return s.httpClient
	}
	return &http.Client{Timeout: sendpulseTimeout}
}

// Send authenticates (reusing a cached OAuth token while it is valid) and
// posts the message to POST /smtp/emails, mirroring
// modulo-financeiro/src/lib/helpers/sendpulse-mail-helper.ts.
func (s *sendpulseSender) Send(p_Msg Message) error {
	token, err := s.ensureToken()
	if err != nil {
		return err
	}

	body := sendpulseEmailRequest{
		Email: sendpulseEmailPayload{
			HTML:    base64.StdEncoding.EncodeToString([]byte(p_Msg.Body)),
			Text:    p_Msg.Body,
			Subject: p_Msg.Subject,
			From: sendpulseContact{
				Name:  s.FromName,
				Email: s.FromEmail,
			},
			To: []sendpulseContact{{Email: p_Msg.To}},
		},
	}

	status, respBody, err := s.postJSON("/smtp/emails", body, token)
	if err != nil {
		return fmt.Errorf("sendpulse send: %w", err)
	}

	var parsed sendpulseEmailResponse
	_ = json.Unmarshal(respBody, &parsed)
	if status != http.StatusOK || !parsed.Result {
		return fmt.Errorf("sendpulse send: unexpected response (HTTP %d): %s", status, truncate(respBody, 300))
	}
	return nil
}

// ensureToken returns a cached OAuth token, requesting a new one if none is
// cached or the cached one is within sendpulseTokenSkew of expiring.
func (s *sendpulseSender) ensureToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.token != "" && time.Now().Before(s.tokenExp) {
		return s.token, nil
	}

	reqBody := map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     s.UserID,
		"client_secret": s.Secret,
	}
	status, respBody, err := s.postJSON("/oauth/access_token", reqBody, "")
	if err != nil {
		return "", fmt.Errorf("sendpulse auth: %w", err)
	}

	var parsed sendpulseTokenResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil || parsed.AccessToken == "" || status != http.StatusOK {
		return "", fmt.Errorf("sendpulse auth: unexpected response (HTTP %d): %s", status, truncate(respBody, 300))
	}

	expiresIn := parsed.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	s.token = parsed.AccessToken
	s.tokenExp = time.Now().Add(time.Duration(expiresIn)*time.Second - sendpulseTokenSkew)
	return s.token, nil
}

func (s *sendpulseSender) postJSON(path string, body interface{}, token string) (int, []byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, sendpulseAPI+path, bytes.NewReader(encoded))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := s.client().Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, buf.Bytes(), nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
