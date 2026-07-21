package qiniu

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sms "github.com/feymanlee/go-sms"
)

func TestSendPostsSignedQiniuJSON(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/message" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := r.Header.Get("Authorization"), expectedAuthorization(r, "access-key", "secret-key", payload); got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		var body struct {
			SignatureID string            `json:"signature_id"`
			TemplateID  string            `json:"template_id"`
			Mobiles     []string          `json:"mobiles"`
			Parameters  map[string]string `json:"parameters"`
		}
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatal(err)
		}
		if body.SignatureID != "sig-1" || body.TemplateID != "template-1" {
			t.Errorf("body = %#v", body)
		}
		if len(body.Mobiles) != 1 || body.Mobiles[0] != "13812345678" {
			t.Errorf("mobiles = %#v", body.Mobiles)
		}
		if got := body.Parameters; len(got) != 2 || got["code"] != "123456" || got["minutes"] != "5" {
			t.Errorf("parameters = %#v", got)
		}
		w.Header().Set("X-Reqid", "request-1")
		_, _ = io.WriteString(w, `{"job_id":"job-1"}`)
	}))
	defer server.Close()

	provider := testProvider(t, server.Client(), server.URL+"/v1/message")
	submission, err := provider.Send(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	if submission.Provider != "qiniu" || submission.MessageID != "job-1" || submission.RequestID != "request-1" || submission.Metadata != nil {
		t.Fatalf("submission = %#v", submission)
	}
}

func TestSendUsesDefaultSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SignatureID string `json:"signature_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.SignatureID != "default-sig" {
			t.Errorf("signature_id = %q", body.SignatureID)
		}
		_, _ = io.WriteString(w, `{"job_id":"job-1"}`)
	}))
	defer server.Close()

	req := testRequest(t)
	req.SignatureRef = ""
	if _, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestSendDoesNotFollowRedirects(t *testing.T) {
	var redirects atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirects.Add(1)
		_, _ = io.WriteString(w, `{"job_id":"unexpected"}`)
	}))
	defer target.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
	if !errors.Is(err, sms.ErrRejected) || redirects.Load() != 0 {
		t.Fatalf("error = %v, redirects = %d", err, redirects.Load())
	}
}

func TestSendClassifiesHTTPResponsesAndCapturesRequestID(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, want: sms.ErrAuthentication},
		{name: "forbidden", status: http.StatusForbidden, want: sms.ErrAuthentication},
		{name: "rate limited", status: http.StatusTooManyRequests, want: sms.ErrRateLimited},
		{name: "unavailable", status: http.StatusBadGateway, want: sms.ErrUnavailable},
		{name: "rejected", status: http.StatusBadRequest, want: sms.ErrRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Reqid", "request-error")
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"error":"failed for +8613812345678 and 13812345678"}`)
			}))
			defer server.Close()

			_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
			var detail *sms.SendError
			if !errors.Is(err, tt.want) || !errors.As(err, &detail) || detail.RequestID != "request-error" || detail.Message != "failed for [recipient] and [recipient]" {
				t.Fatalf("error = %v, detail = %#v", err, detail)
			}
		})
	}
}

func TestSendRejectsMalformedSuccess(t *testing.T) {
	for _, body := range []string{"not JSON for +8613812345678", `{}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Reqid", "request-malformed")
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()

			_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
			var detail *sms.SendError
			if !errors.Is(err, sms.ErrInternal) || !errors.As(err, &detail) || detail.RequestID != "request-malformed" || strings.Contains(detail.Message, "13812345678") {
				t.Fatalf("error = %v, detail = %#v", err, detail)
			}
		})
	}
}

func TestSendRejectsNonChinaRecipientWithoutHTTPAttempt(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected")
	})}
	provider := testProvider(t, client, "https://example.invalid/v1/message")
	req := testRequest(t)
	recipient, err := sms.ParseRecipient("+12025550123")
	if err != nil {
		t.Fatal(err)
	}
	req.Recipient = recipient

	_, err = provider.Send(context.Background(), req)
	if !errors.Is(err, sms.ErrInvalidRequest) || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestSendReturnsUnknownOutcomeAfterTransportError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cause := errors.New("connection reset for +8613812345678")
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		cancel()
		return nil, cause
	})}

	_, err := testProvider(t, client, "https://example.invalid/v1/message").Send(ctx, testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || !errors.As(err, &detail) || strings.Contains(detail.Message, "13812345678") || calls.Load() != 1 {
		t.Fatalf("error = %v, detail = %#v, calls = %d", err, detail, calls.Load())
	}
}

func TestSendDoesNotCallTransportForCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected")
	})}

	_, err := testProvider(t, client, "https://example.invalid/v1/message").Send(ctx, testRequest(t))
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestSendDoesNotExposeSecretTransportError(t *testing.T) {
	raw := &secretQiniuTransportError{recipient: "+8613812345678"}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, raw
	})}

	_, err := testProvider(t, client, "https://example.invalid/v1/message").Send(context.Background(), testRequest(t))
	if !errors.Is(err, raw) || strings.Contains(err.Error(), raw.recipient) {
		t.Fatalf("error = %v", err)
	}
	var recovered *secretQiniuTransportError
	if errors.As(err, &recovered) {
		t.Fatalf("raw transport error leaked through error chain: %#v", recovered)
	}
	if unwrap := errors.Unwrap(err); unwrap == nil || unwrap == raw || !strings.Contains(unwrap.Error(), "connection reset for [recipient]") || errors.Unwrap(unwrap) != nil || strings.Contains(unwrap.Error(), raw.recipient) {
		t.Fatalf("unwrap = %#v", unwrap)
	}
}

func TestNewValidatesCredentials(t *testing.T) {
	for _, config := range []Config{
		{SecretKey: "secret-key"},
		{AccessKey: "access-key"},
	} {
		if _, err := New(config); err == nil {
			t.Fatal("New returned nil error")
		}
	}
}

func testProvider(t *testing.T, client *http.Client, endpoint string) *Provider {
	t.Helper()
	provider, err := New(Config{AccessKey: "access-key", SecretKey: "secret-key", DefaultSignatureRef: "default-sig"}, WithHTTPClient(client), WithEndpoint(endpoint))
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func testRequest(t *testing.T) sms.Request {
	t.Helper()
	recipient, err := sms.ParseRecipient("+8613812345678")
	if err != nil {
		t.Fatal(err)
	}
	return sms.Request{
		Recipient: recipient,
		Message: sms.TemplateMessage{TemplateID: "template-1", Params: []sms.TemplateParam{
			{Name: "code", Value: "123456"}, {Name: "minutes", Value: "5"},
		}},
		SignatureRef: "sig-1",
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func expectedAuthorization(req *http.Request, accessKey, secretKey string, body []byte) string {
	host := req.Host
	canonical := req.Method + " " + req.URL.EscapedPath()
	if req.URL.RawQuery != "" {
		canonical += "?" + req.URL.RawQuery
	}
	canonical += "\nHost: " + host
	if contentType := req.Header.Get("Content-Type"); contentType != "" {
		canonical += "\nContent-Type: " + contentType
	}
	canonical += "\n\n" + string(body)

	mac := hmac.New(sha1.New, []byte(secretKey))
	_, _ = mac.Write([]byte(canonical))
	return "Qiniu " + accessKey + ":" + base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

type secretQiniuTransportError struct {
	recipient string
}

func (e *secretQiniuTransportError) Error() string {
	return "connection reset for " + e.recipient
}
