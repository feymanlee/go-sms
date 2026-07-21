package ucloud

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sms "github.com/feymanlee/go-sms"
)

func TestSendPostsSignedUSMSForm(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"Action": "SendUSMSMessage", "PhoneNumbers.0": "(86)13812345678",
			"TemplateParams.0": "123456", "TemplateParams.1": "5",
			"TemplateId": "template-1", "SigContent": "Example", "ProjectId": "project-1",
			"Region": "cn-bj2", "PublicKey": "public-1",
		}
		for key, value := range want {
			if got := r.Form.Get(key); got != value {
				t.Errorf("form[%q] = %q, want %q", key, got, value)
			}
		}
		if got := r.Form.Get("Signature"); got == "" {
			t.Error("Signature is empty")
		}
		_, _ = io.WriteString(w, `{"RetCode":0,"Message":"","SessionNo":"session-1"}`)
	}))
	defer server.Close()

	provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	submission, err := provider.Send(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	if submission.Provider != "ucloud" || submission.MessageID != "session-1" || submission.RequestID != "" {
		t.Fatalf("submission = %#v", submission)
	}
}

func TestSendUsesDefaultSignature(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("SigContent"); got != "Default" {
			t.Errorf("SigContent = %q", got)
		}
		_, _ = io.WriteString(w, `{"RetCode":0,"SessionNo":"session-1"}`)
	}))
	defer server.Close()

	provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	req := testRequest(t)
	req.SignatureRef = ""
	if _, err := provider.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestSendClassifiesHTTPResponses(t *testing.T) {
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
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, "response must not leak")
			}))
			defer server.Close()
			provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Send(context.Background(), testRequest(t))
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSendClassifiesProviderRejectionAndSanitizesMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"RetCode":170,"Message":"rejected +8613812345678"}`)
	}))
	defer server.Close()
	provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(context.Background(), testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrRejected) || !errors.As(err, &detail) || detail.Code != "170" || strings.Contains(detail.Message, "13812345678") {
		t.Fatalf("error = %v, detail = %#v", err, detail)
	}
}

func TestSendRejectsMalformedSuccessfulResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: "not JSON for +8613812345678"},
		{name: "missing session", body: `{"RetCode":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			provider, err := New(testConfig(), WithHTTPClient(server.Client()), WithEndpoint(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Send(context.Background(), testRequest(t))
			var detail *sms.SendError
			if !errors.Is(err, sms.ErrInternal) || !errors.As(err, &detail) || strings.Contains(detail.Message, "13812345678") {
				t.Fatalf("error = %v, detail = %#v", err, detail)
			}
		})
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
	provider, err := New(testConfig(), WithHTTPClient(client), WithEndpoint("https://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(ctx, testRequest(t))
	var detail *sms.SendError
	if !errors.Is(err, sms.ErrUnknownOutcome) || !errors.Is(err, cause) || !errors.Is(err, context.Canceled) || !errors.As(err, &detail) || strings.Contains(detail.Message, "13812345678") || calls.Load() != 1 {
		t.Fatalf("error = %v, detail = %#v", err, detail)
	}
}

func TestSendRejectsCanceledContextWithoutAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected")
	})}
	provider, err := New(testConfig(), WithHTTPClient(client), WithEndpoint("https://example.invalid"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = provider.Send(ctx, testRequest(t))
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestNewValidatesRequiredConfig(t *testing.T) {
	valid := testConfig()
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "public key", mutate: func(c *Config) { c.PublicKey = " " }},
		{name: "private key", mutate: func(c *Config) { c.PrivateKey = "" }},
		{name: "project ID", mutate: func(c *Config) { c.ProjectID = "" }},
		{name: "region", mutate: func(c *Config) { c.Region = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			tt.mutate(&config)
			if _, err := New(config); err == nil {
				t.Fatal("New returned nil error")
			}
		})
	}
}

func testConfig() Config {
	return Config{
		PublicKey: "public-1", PrivateKey: "private-1", ProjectID: "project-1", Region: "cn-bj2", DefaultSignatureRef: "Default",
	}
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
		SignatureRef: "Example",
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
