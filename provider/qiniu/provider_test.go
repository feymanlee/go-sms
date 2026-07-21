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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sms "github.com/feymanlee/go-sms"
	"github.com/feymanlee/go-sms/failure"
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
	requireFailure(t, err, failure.Rejected)
	if redirects.Load() != 0 {
		t.Fatalf("error = %v, redirects = %d", err, redirects.Load())
	}
}

func TestSendClassifiesHTTPResponsesAndCapturesRequestID(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		category failure.Category
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, category: failure.Authentication},
		{name: "forbidden", status: http.StatusForbidden, category: failure.Authentication},
		{name: "rate limited", status: http.StatusTooManyRequests, category: failure.RateLimited},
		{name: "unavailable", status: http.StatusBadGateway, category: failure.Unavailable},
		{name: "rejected", status: http.StatusBadRequest, category: failure.Rejected},
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
			got := requireFailure(t, err, tt.category)
			if details := got.Details(); details.Code != strconv.Itoa(tt.status) || details.RequestID != "request-error" {
				t.Fatalf("details = %#v", details)
			}
		})
	}
}

func TestSendDoesNotExposeRemoteErrorBody(t *testing.T) {
	const remoteError = `request for +8613812345678 and +12025550123 failed: Authorization: Bearer qiniu-secret-token; body={"signature_id":"sig-private","template_id":"template-private","mobiles":["13812345678"]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Reqid", "request-error")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":`+strconv.Quote(remoteError)+`}`)
	}))
	defer server.Close()

	_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
	got := requireFailure(t, err, failure.Rejected)
	if details := got.Details(); details.Code != strconv.Itoa(http.StatusBadRequest) || details.RequestID != "request-error" {
		t.Fatalf("details = %#v", details)
	}
	for _, secret := range []string{
		"+8613812345678",
		"+12025550123",
		"Authorization: Bearer qiniu-secret-token",
		`{"signature_id":"sig-private","template_id":"template-private","mobiles":["13812345678"]}`,
	} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("remote error leaked %q: error=%q", secret, err.Error())
		}
	}
}

func TestSendRejectsMalformedSuccess(t *testing.T) {
	for _, body := range []string{
		"not JSON for +8613812345678",
		`{}`,
		`{"job_id":"job-1"} trailing garbage`,
		`{"job_id":"job-1"}{"job_id":"job-2"}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Reqid", "request-malformed")
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()

			_, err := testProvider(t, server.Client(), server.URL).Send(context.Background(), testRequest(t))
			got := requireFailure(t, err, failure.UnknownOutcome)
			if details := got.Details(); details.RequestID != "request-malformed" {
				t.Fatalf("details = %#v", details)
			}
			if strings.Contains(err.Error(), "13812345678") {
				t.Fatalf("error leaked recipient: %v", err)
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
	requireFailure(t, err, failure.UnknownOutcome)
	if errors.Is(err, cause) || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "13812345678") || calls.Load() != 1 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestSendReturnsUnknownOutcomeForInvalidRoundTripperResult(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	})}

	_, err := testProvider(t, client, "https://example.invalid/v1/message").Send(context.Background(), testRequest(t))
	requireFailure(t, err, failure.UnknownOutcome)
}

func TestSendRecordsTransportContextEvidence(t *testing.T) {
	tests := []struct {
		name    string
		context func() (context.Context, context.CancelFunc)
		want    error
	}{
		{
			name:    "canceled",
			context: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			want:    context.Canceled,
		},
		{
			name: "deadline exceeded",
			context: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 10*time.Millisecond)
			},
			want: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.context()
			defer cancel()
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if tt.want == context.Canceled {
					cancel()
				} else {
					<-req.Context().Done()
				}
				return nil, errors.New("transport failure")
			})}

			_, err := testProvider(t, client, "https://example.invalid/v1/message").Send(ctx, testRequest(t))
			requireFailure(t, err, failure.UnknownOutcome)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
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
	requireFailure(t, err, failure.UnknownOutcome)
	if errors.Is(err, raw) || strings.Contains(err.Error(), raw.recipient) {
		t.Fatalf("error = %v", err)
	}
	var recovered *secretQiniuTransportError
	if errors.As(err, &recovered) {
		t.Fatalf("raw transport error leaked through error chain: %#v", recovered)
	}
	if unwrap := errors.Unwrap(err); unwrap != nil {
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

func requireFailure(t *testing.T, err error, category failure.Category) failure.Failure {
	t.Helper()
	got, ok := failure.From(err)
	if !ok {
		t.Fatalf("error is not a Failure: %v", err)
	}
	if got.Category() != category {
		t.Fatalf("category=%q want=%q", got.Category(), category)
	}
	return got
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
