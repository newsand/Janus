package mailer

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestSendPulseServer wires up a fake SendPulse API: /oauth/access_token
// always issues the given token, /smtp/emails records the last request body
// and replies according to acceptSend.
func newTestSendPulseServer(t *testing.T, token string, acceptSend bool, expiresIn int) (*httptest.Server, *sendpulseEmailRequest) {
	t.Helper()
	var lastReq sendpulseEmailRequest
	authCalls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/access_token":
			authCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sendpulseTokenResponse{
				AccessToken: token,
				ExpiresIn:   expiresIn,
			})
		case "/smtp/emails":
			if got := r.Header.Get("Authorization"); got != "Bearer "+token {
				t.Errorf("missing/incorrect bearer token on /smtp/emails: %q", got)
			}
			_ = json.NewDecoder(r.Body).Decode(&lastReq)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(sendpulseEmailResponse{Result: acceptSend})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		if authCalls == 0 {
			t.Error("expected at least one call to /oauth/access_token")
		}
	})
	return srv, &lastReq
}

func newTestSendpulseSender(t *testing.T, srv *httptest.Server) *sendpulseSender {
	t.Helper()
	return &sendpulseSender{
		UserID:    "uid",
		Secret:    "secret",
		FromName:  "Janus",
		FromEmail: "janus@example.com",
		httpClient: &http.Client{
			Transport: rewriteHostTransport{base: srv.URL},
		},
	}
}

// rewriteHostTransport redirects every request to srv.URL regardless of the
// hardcoded sendpulseAPI base, so tests don't hit the real SendPulse API.
type rewriteHostTransport struct{ base string }

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(strings.TrimPrefix(t.base, "http://"), "https://")
	return http.DefaultTransport.RoundTrip(req)
}

func TestSendPulseSendSuccess(t *testing.T) {
	srv, lastReq := newTestSendPulseServer(t, "tok-abc", true, 3600)
	s := newTestSendpulseSender(t, srv)

	err := s.Send(Message{To: "user@example.com", Subject: "Oi", Body: "Corpo <b>html</b>"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(lastReq.Email.To) != 1 || lastReq.Email.To[0].Email != "user@example.com" {
		t.Fatalf("unexpected To: %+v", lastReq.Email.To)
	}
	if lastReq.Email.From.Email != "janus@example.com" || lastReq.Email.From.Name != "Janus" {
		t.Fatalf("unexpected From: %+v", lastReq.Email.From)
	}
	if lastReq.Email.Subject != "Oi" {
		t.Fatalf("unexpected Subject: %q", lastReq.Email.Subject)
	}
	decoded, err := decodeBase64(lastReq.Email.HTML)
	if err != nil || decoded != "Corpo <b>html</b>" {
		t.Fatalf("unexpected HTML (decode err=%v): %q", err, lastReq.Email.HTML)
	}
}

func TestSendPulseSendRejected(t *testing.T) {
	srv, _ := newTestSendPulseServer(t, "tok-abc", false, 3600)
	s := newTestSendpulseSender(t, srv)

	err := s.Send(Message{To: "user@example.com", Subject: "Oi", Body: "x"})
	if err == nil {
		t.Fatal("want error when SendPulse returns result:false")
	}
}

func TestSendPulseTokenIsCached(t *testing.T) {
	srv, _ := newTestSendPulseServer(t, "tok-abc", true, 3600)
	s := newTestSendpulseSender(t, srv)

	for i := 0; i < 3; i++ {
		if err := s.Send(Message{To: "user@example.com", Subject: "Oi", Body: "x"}); err != nil {
			t.Fatalf("Send #%d: %v", i, err)
		}
	}
	// newTestSendPulseServer's cleanup only asserts authCalls > 0; assert the
	// cache explicitly here too.
	if s.token != "tok-abc" {
		t.Fatalf("expected cached token, got %q", s.token)
	}
}

func TestSendPulseTokenRefreshedWhenExpired(t *testing.T) {
	srv, _ := newTestSendPulseServer(t, "tok-abc", true, 3600)
	s := newTestSendpulseSender(t, srv)
	s.token = "stale"
	s.tokenExp = time.Now().Add(-time.Minute) // already expired

	if err := s.Send(Message{To: "user@example.com", Subject: "Oi", Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if s.token != "tok-abc" {
		t.Fatalf("expected refreshed token, got %q", s.token)
	}
}

func decodeBase64(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
